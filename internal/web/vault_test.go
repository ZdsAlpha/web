package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestVaultProxyRequiresTokenAndProxiesEncryptedRange(t *testing.T) {
	const (
		accessToken = "prototype-token"
		key         = "abc_DEF-/zyx"
	)

	var authCalls, downloadCalls int
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/authorize":
			authCalls++
			user, password, ok := r.BasicAuth()
			if !ok || user != "key-id" || password != "application-key" {
				return response(http.StatusUnauthorized, nil, "bad credentials"), nil
			}
			payload, _ := json.Marshal(map[string]string{
				"authorizationToken": "b2-token",
				"downloadUrl":        "https://f000.backblazeb2.com/download",
			})
			return response(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, string(payload)), nil
		case "/download/file/private-bucket/abc_DEF-/zyx":
			downloadCalls++
			if r.Header.Get("Authorization") != "b2-token" {
				return response(http.StatusUnauthorized, nil, "bad token"), nil
			}
			if got, want := r.Header.Get("Range"), "bytes=2-5"; got != want {
				t.Fatalf("B2 Range = %q; want %q", got, want)
			}
			return response(http.StatusPartialContent, http.Header{
				"Accept-Ranges":            []string{"bytes"},
				"Content-Length":           []string{"4"},
				"Content-Range":            []string{"bytes 2-5/10"},
				"Content-Type":             []string{"video/mp4"},
				"X-Bz-Info-s3zero-iv":      []string{"0011223344556677"},
				"X-Bz-Info-s3zero-keyhash": []string{"deadbeef"},
			}, "cdef"), nil
		default:
			return response(http.StatusNotFound, nil, "not found"), nil
		}
	})

	proxy := NewVaultProxy(VaultConfig{
		B2KeyID:       "key-id",
		B2Application: "application-key",
		B2Bucket:      "private-bucket",
		AccessToken:   accessToken,
		AuthorizeURL:  "https://api.backblazeb2.com/authorize",
		HTTPClient:    &http.Client{Transport: transport},
	})

	missingToken := httptest.NewRecorder()
	proxy.ServeHTTP(missingToken, httptest.NewRequest(http.MethodGet, "/vault/object?key="+key, nil))
	if missingToken.Code != http.StatusNotFound {
		t.Fatalf("missing token status = %d; want %d", missingToken.Code, http.StatusNotFound)
	}
	if authCalls != 0 {
		t.Fatalf("missing token made %d authorization calls; want 0", authCalls)
	}

	req := httptest.NewRequest(http.MethodGet, "/vault/object?key="+key, nil)
	req.Header.Set("X-Vault-Access-Token", accessToken)
	req.Header.Set("Range", "bytes=2-5")
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d; want %d", rec.Code, http.StatusPartialContent)
	}
	if got, want := rec.Body.String(), "cdef"; got != want {
		t.Fatalf("range body = %q; want %q", got, want)
	}
	for header, want := range map[string]string{
		"Cache-Control":    "no-store",
		"X-Robots-Tag":     "noindex, nofollow, noarchive",
		"Content-Range":    "bytes 2-5/10",
		"Content-Type":     "video/mp4",
		"X-S3Zero-IV":      "0011223344556677",
		"X-S3Zero-KeyHash": "deadbeef",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q; want %q", header, got, want)
		}
	}
	if authCalls != 1 || downloadCalls != 1 {
		t.Fatalf("B2 calls = authorize %d, download %d; want 1, 1", authCalls, downloadCalls)
	}
}

func TestVaultProxyRejectsInvalidKeysBeforeStorageAccess(t *testing.T) {
	proxy := NewVaultProxy(VaultConfig{
		B2KeyID:       "key-id",
		B2Application: "application-key",
		B2Bucket:      "private-bucket",
		AccessToken:   "prototype-token",
		AuthorizeURL:  "http://127.0.0.1:1/authorize",
	})

	for _, key := range []string{"", "plain name.txt", "../secret", "abc//def", "abc?x=y"} {
		req := httptest.NewRequest(http.MethodGet, "/vault/object?key="+url.QueryEscape(key), nil)
		req.Header.Set("X-Vault-Access-Token", "prototype-token")
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("key %q status = %d; want %d", key, rec.Code, http.StatusBadRequest)
		}
	}
}

func TestS3ProxyListsBucketsWithoutPersistingCredentials(t *testing.T) {
	var got *http.Request
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return response(http.StatusOK, http.Header{"Content-Type": []string{"application/xml"}}, `<ListAllMyBucketsResult><Buckets><Bucket><Name>archive</Name></Bucket></Buckets></ListAllMyBucketsResult>`), nil
	})
	proxy := NewVaultProxy(VaultConfig{
		S3ProxyEnabled: true,
		HTTPClient:     &http.Client{Transport: transport},
	})

	body := `{"endpoint":"https://s3.example.test","region":"us-east-1","accessKeyId":"AKIA_TEST","secretAccessKey":"secret-value","query":[["list-type","2"]]}`
	req := httptest.NewRequest(http.MethodPost, "/vault/proxy/list-buckets", strings.NewReader(body))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list buckets status = %d; want %d (%s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got == nil {
		t.Fatal("proxy did not make an upstream request")
	}
	if got.Method != http.MethodGet || got.URL.Path != "/" || got.URL.RawQuery != "list-type=2" {
		t.Fatalf("upstream request = %s %s?%s; want GET /?list-type=2", got.Method, got.URL.Path, got.URL.RawQuery)
	}
	if got.Header.Get("Authorization") == "" || !strings.Contains(got.Header.Get("Authorization"), "Credential=AKIA_TEST/") {
		t.Fatalf("upstream request is not SigV4-signed: %q", got.Header.Get("Authorization"))
	}
	if strings.Contains(got.URL.String(), "secret-value") || strings.Contains(rec.Body.String(), "secret-value") {
		t.Fatal("secret access key leaked into the upstream URL or response")
	}
}

func TestS3ProxyForwardsEncryptedObjectRangeAndEnvelope(t *testing.T) {
	var got *http.Request
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.Clone(r.Context())
		return response(http.StatusPartialContent, http.Header{
			"Accept-Ranges":             []string{"bytes"},
			"Content-Length":            []string{"4"},
			"Content-Range":             []string{"bytes 2-5/10"},
			"Content-Type":              []string{"video/mp4"},
			"X-Amz-Meta-S3zero-Iv":      []string{"0011223344556677"},
			"X-Amz-Meta-S3zero-Keyhash": []string{"deadbeef"},
		}, "cdef"), nil
	})
	proxy := NewVaultProxy(VaultConfig{
		S3ProxyEnabled: true,
		HTTPClient:     &http.Client{Transport: transport},
	})

	body := `{"endpoint":"https://s3.example.test/root","region":"us-east-1","accessKeyId":"AKIA_TEST","secretAccessKey":"secret-value","pathStyle":true,"bucket":"archive","key":"abc_DEF-/zyx","range":"bytes=2-5"}`
	req := httptest.NewRequest(http.MethodPost, "/vault/proxy/object", strings.NewReader(body))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent || rec.Body.String() != "cdef" {
		t.Fatalf("object response = %d %q; want 206 cdef", rec.Code, rec.Body.String())
	}
	if got == nil {
		t.Fatal("proxy did not make an upstream request")
	}
	if got.URL.Path != "/root/archive/abc_DEF-/zyx" || got.Header.Get("Range") != "bytes=2-5" {
		t.Fatalf("upstream object request = %s, Range=%q", got.URL.Path, got.Header.Get("Range"))
	}
	if got.Header.Get("Authorization") == "" || got.Header.Get("X-Amz-Date") == "" {
		t.Fatal("upstream object request is missing SigV4 headers")
	}
	if got := rec.Header().Get("X-S3Zero-IV"); got != "0011223344556677" {
		t.Fatalf("X-S3Zero-IV = %q; want envelope metadata", got)
	}
	if got := rec.Header().Get("X-S3Zero-KeyHash"); got != "deadbeef" {
		t.Fatalf("X-S3Zero-KeyHash = %q; want envelope metadata", got)
	}
}

func TestS3ProxyRejectsInsecureEndpoint(t *testing.T) {
	var calls int
	proxy := NewVaultProxy(VaultConfig{
		S3ProxyEnabled: true,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return response(http.StatusOK, nil, "unexpected"), nil
		})},
	})

	body := `{"endpoint":"https://s3.example.test","region":"us-east-1","accessKeyId":"AKIA_TEST","secretAccessKey":"secret-value"}`
	httpEndpoint := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/vault/proxy/list-buckets", strings.NewReader(strings.Replace(body, "https://", "http://", 1)))
	proxy.ServeHTTP(httpEndpoint, req)
	if httpEndpoint.Code != http.StatusBadRequest || !strings.Contains(httpEndpoint.Body.String(), "must use https") {
		t.Fatalf("http endpoint response = %d %q; want HTTPS validation failure", httpEndpoint.Code, httpEndpoint.Body.String())
	}
	if calls != 0 {
		t.Fatalf("proxy made %d upstream requests; want 0", calls)
	}
}

func TestS3ProxyIsDisabledByDefault(t *testing.T) {
	proxy := NewVaultProxy(VaultConfig{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, httptest.NewRequest(method, "/vault/proxy/status", strings.NewReader(`{}`)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("disabled status method %s = %d; want %d", method, rec.Code, http.StatusNotFound)
		}
	}
}

func TestS3ProxyEnabledWithoutAccessToken(t *testing.T) {
	proxy := NewVaultProxy(VaultConfig{S3ProxyEnabled: true})
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/vault/proxy/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d; want %d", rec.Code, http.StatusOK)
	}
}

func TestVaultRouteIsNotInSitemap(t *testing.T) {
	h := testHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if strings.Contains(rec.Body.String(), "/vault") {
		t.Fatalf("sitemap mentions hidden vault route: %s", rec.Body.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, headers http.Header, body string) *http.Response {
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
