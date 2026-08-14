package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ZdsAlpha/web/internal/content"
	"github.com/ZdsAlpha/web/view"
)

const defaultB2AuthorizeURL = "https://api.backblazeb2.com/b2api/v4/b2_authorize_account"

var encryptedObjectKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:/[A-Za-z0-9_-]+)*$`)

// VaultConfig configures the disabled-by-default S3 Zero read prototype.
//
// The access token is an application-level gate for this prototype; it is not
// an S3 Zero encryption key and is never sent to Backblaze. The Backblaze
// application key remains server-side and is used only for encrypted range
// reads.
type VaultConfig struct {
	B2KeyID        string
	B2Application  string
	B2Bucket       string
	AccessToken    string
	AuthorizeURL   string
	S3ProxyEnabled bool
	HTTPClient     *http.Client
}

// HandlerWithVault builds the site's handler and optionally mounts the hidden
// vault prototype. The normal Handler intentionally leaves the prototype
// unmounted so existing callers and tests remain read-only by default.
func HandlerWithVault(store *content.Store, staticFS fs.FS, baseURL string, vault http.Handler) http.Handler {
	mux := http.NewServeMux()

	// noDirFS disables directory listings; only files are served.
	staticSrv := cacheStatic(http.FileServerFS(noDirFS{staticFS}))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticSrv))

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("GET /robots.txt", robots(baseURL))
	mux.HandleFunc("GET /sitemap.xml", sitemap(store, baseURL))

	if vault != nil {
		// The account workspace is intentionally unlinked and is never included
		// in the content-generated sitemap. Account operations happen in the
		// browser; the storage proxy below is a separate, isolated surface.
		mux.Handle("GET /vault", vaultSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			render(w, r, http.StatusOK, view.Vault())
		})))
		mux.Handle("/vault/object", vault)
		mux.Handle("/vault/proxy", vault)
		mux.Handle("/vault/proxy/", vault)
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, http.StatusOK, view.Home(store.Posts()))
	})

	mux.HandleFunc("GET /posts/{slug}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := store.Post(r.PathValue("slug"))
		if !ok {
			notFound(w, r)
			return
		}
		render(w, r, http.StatusOK, view.Post(p))
	})

	// Standalone pages live at the root (e.g. /about). Registered as a
	// catch-all so it runs last; unknown slugs fall through to 404.
	mux.HandleFunc("GET /{slug}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := store.Page(r.PathValue("slug"))
		if !ok {
			notFound(w, r)
			return
		}
		render(w, r, http.StatusOK, view.Page(p))
	})

	return securityHeaders(canonicalHostRedirect(mux, baseURL))
}

// NewVaultProxy returns the isolated vault handler. The legacy B2 object route
// returns 404 when its required configuration is absent; the generic S3 proxy
// is separately controlled by S3ProxyEnabled.
func NewVaultProxy(cfg VaultConfig) http.Handler {
	proxy := &vaultProxy{
		accessToken: cfg.AccessToken,
		bucket:      cfg.B2Bucket,
		b2: &b2Client{
			keyID:       cfg.B2KeyID,
			application: cfg.B2Application,
			authorize:   firstNonEmpty(cfg.AuthorizeURL, defaultB2AuthorizeURL),
			httpClient:  firstHTTPClient(cfg.HTTPClient),
		},
		s3: newS3Proxy(cfg.S3ProxyEnabled, cfg.HTTPClient),
	}
	proxy.enabled = proxy.accessToken != "" && proxy.bucket != "" && proxy.b2.keyID != "" && proxy.b2.application != ""
	return proxy
}

type vaultProxy struct {
	enabled     bool
	accessToken string
	bucket      string
	b2          *b2Client
	s3          *s3Proxy
}

func (p *vaultProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/vault/proxy" || strings.HasPrefix(r.URL.Path, "/vault/proxy/") {
		p.s3.ServeHTTP(w, r)
		return
	}
	p.setResponseHeaders(w)

	if !p.enabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !tokenMatches(r.Header.Get("X-Vault-Access-Token"), p.accessToken) {
		// Keep this prototype difficult to discover and do not distinguish a
		// bad token from a disabled route.
		http.NotFound(w, r)
		return
	}

	key := r.URL.Query().Get("key")
	if !validEncryptedObjectKey(key) {
		http.Error(w, "invalid encrypted object key", http.StatusBadRequest)
		return
	}

	response, err := p.b2.download(r.Context(), p.bucket, key, r.Method, r.Header.Get("Range"))
	if err != nil {
		http.Error(w, "vault storage unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		http.NotFound(w, r)
		return
	}
	if response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		copyVaultHeaders(w, response.Header)
		w.WriteHeader(response.StatusCode)
		return
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		http.Error(w, "vault storage request failed", http.StatusBadGateway)
		return
	}

	copyVaultHeaders(w, response.Header)
	// Range responses may remain open while the browser's media element asks
	// for later chunks; do not let the public site's short write timeout abort
	// this isolated vault stream.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.WriteHeader(response.StatusCode)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, response.Body)
	}
}

func (p *vaultProxy) setResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Vary", "Range")
}

func tokenMatches(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func validEncryptedObjectKey(key string) bool {
	return len(key) <= 4096 && encryptedObjectKeyPattern.MatchString(key)
}

func copyVaultHeaders(dst http.ResponseWriter, src http.Header) {
	for _, name := range []string{
		"Accept-Ranges",
		"Content-Length",
		"Content-Range",
		"Content-Type",
		"ETag",
		"Last-Modified",
	} {
		for _, value := range src.Values(name) {
			dst.Header().Add(name, value)
		}
	}

	// B2's Native API exposes S3 user metadata as X-Bz-Info-* headers. Map
	// only the protocol envelope fields needed by the browser decryptor.
	for source, target := range map[string]string{
		"x-bz-info-s3zero-iv":      "X-S3Zero-IV",
		"x-bz-info-s3zero-keyhash": "X-S3Zero-KeyHash",
	} {
		for name, values := range src {
			if strings.ToLower(name) != source {
				continue
			}
			for _, value := range values {
				dst.Header().Add(target, value)
			}
		}
	}
}

type b2Client struct {
	keyID       string
	application string
	authorize   string
	httpClient  *http.Client

	mu        sync.Mutex
	auth      b2Authorization
	authUntil time.Time
}

type b2Authorization struct {
	Token       string `json:"authorizationToken"`
	DownloadURL string `json:"downloadUrl"`
}

func (c *b2Client) download(ctx context.Context, bucket, key, method, rangeHeader string) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		auth, err := c.authorization(ctx)
		if err != nil {
			return nil, err
		}

		downloadURL, err := b2ObjectURL(auth.DownloadURL, bucket, key)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, method, downloadURL, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", auth.Token)
		request.Header.Set("Accept-Encoding", "identity")
		if rangeHeader != "" {
			request.Header.Set("Range", rangeHeader)
		}

		response, err := c.httpClient.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode != http.StatusUnauthorized || attempt == 1 {
			return response, nil
		}
		_ = response.Body.Close()
		c.invalidate(auth.Token)
	}

	return nil, errors.New("unreachable authorization retry")
}

func (c *b2Client) authorization(ctx context.Context) (b2Authorization, error) {
	c.mu.Lock()
	if c.auth.Token != "" && time.Now().Before(c.authUntil) {
		auth := c.auth
		c.mu.Unlock()
		return auth, nil
	}
	c.mu.Unlock()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.authorize, nil)
	if err != nil {
		return b2Authorization{}, err
	}
	request.SetBasicAuth(c.keyID, c.application)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return b2Authorization{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return b2Authorization{}, fmt.Errorf("b2 authorization returned %s", response.Status)
	}

	var auth b2Authorization
	if err := json.NewDecoder(response.Body).Decode(&auth); err != nil {
		return b2Authorization{}, fmt.Errorf("decode b2 authorization: %w", err)
	}
	if auth.Token == "" || auth.DownloadURL == "" {
		return b2Authorization{}, errors.New("b2 authorization response missing download credentials")
	}

	c.mu.Lock()
	c.auth = auth
	// B2 authorization tokens normally last 24 hours. Refresh early so an
	// in-flight request does not cross the provider's expiry boundary.
	c.authUntil = time.Now().Add(23 * time.Hour)
	c.mu.Unlock()
	return auth, nil
}

func (c *b2Client) invalidate(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.auth.Token == token {
		c.auth = b2Authorization{}
		c.authUntil = time.Time{}
	}
}

func b2ObjectURL(downloadURL, bucket, key string) (string, error) {
	base, err := url.Parse(downloadURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", errors.New("invalid b2 download URL")
	}
	if bucket == "" || strings.Contains(bucket, "/") || !validB2Bucket(bucket) {
		return "", errors.New("invalid b2 bucket")
	}
	if !validEncryptedObjectKey(key) {
		return "", errors.New("invalid encrypted object key")
	}

	pathParts := []string{strings.TrimRight(base.Path, "/"), "file", url.PathEscape(bucket)}
	for _, segment := range strings.Split(key, "/") {
		pathParts = append(pathParts, url.PathEscape(segment))
	}
	base.Path = strings.Join(pathParts, "/")
	return base.String(), nil
}

func validB2Bucket(bucket string) bool {
	if len(bucket) < 6 || len(bucket) > 50 {
		return false
	}
	for _, char := range bucket {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '.' {
			return false
		}
	}
	return true
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func firstHTTPClient(client *http.Client) *http.Client {
	if client != nil {
		return client
	}
	return &http.Client{Timeout: 30 * time.Second}
}
