package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// s3Proxy is an opt-in, stateless S3-compatible proxy for browsers whose
// storage provider does not allow CORS. Credentials are accepted per request,
// never persisted, and are never placed in a URL or response.
type s3Proxy struct {
	enabled     bool
	accessToken string
	client      *http.Client
}

type s3ProxyRequest struct {
	Endpoint        string     `json:"endpoint"`
	Region          string     `json:"region"`
	AccessKeyID     string     `json:"accessKeyId"`
	SecretAccessKey string     `json:"secretAccessKey"`
	SessionToken    string     `json:"sessionToken"`
	PathStyle       bool       `json:"pathStyle"`
	Bucket          string     `json:"bucket"`
	Key             string     `json:"key"`
	Range           string     `json:"range"`
	Query           [][]string `json:"query"`
}

func newS3Proxy(enabled bool, accessToken string, client *http.Client) *s3Proxy {
	if client == nil {
		client = &http.Client{Transport: safeS3Transport()}
	}
	return &s3Proxy{
		enabled:     enabled && accessToken != "",
		accessToken: accessToken,
		client:      client,
	}
}

func (p *s3Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setVaultProxyHeaders(w)
	if r.URL.Path == "/vault/proxy/status" {
		if !p.enabled || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"enabled":true}`)
		return
	}
	if !p.enabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !tokenMatches(r.Header.Get("X-Vault-Access-Token"), p.accessToken) {
		// Do not distinguish an invalid gate token from an unmounted proxy.
		http.NotFound(w, r)
		return
	}

	var request s3ProxyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid proxy request", http.StatusBadRequest)
		return
	}
	if err := validateS3ProxyRequest(request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	operation := strings.TrimPrefix(r.URL.Path, "/vault/proxy/")
	var response *http.Response
	var err error
	switch operation {
	case "list-buckets":
		response, err = p.get(r, request, "", "", request.Query)
	case "list-objects":
		if request.Bucket == "" {
			http.Error(w, "bucket is required", http.StatusBadRequest)
			return
		}
		response, err = p.get(r, request, request.Bucket, "", request.Query)
	case "object":
		if request.Bucket == "" || !validEncryptedObjectKey(request.Key) {
			http.Error(w, "bucket and encrypted object key are required", http.StatusBadRequest)
			return
		}
		response, err = p.get(r, request, request.Bucket, request.Key, nil)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "storage proxy unavailable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		copyS3ProxyHeaders(w, response.Header)
		w.WriteHeader(response.StatusCode)
		_, _ = io.CopyN(w, response.Body, 8<<10)
		return
	}

	// Remove the server write deadline for only this streaming response. The
	// public site's normal timeout remains unchanged.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	copyS3ProxyHeaders(w, response.Header)
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (p *s3Proxy) get(r *http.Request, input s3ProxyRequest, bucket, key string, query [][]string) (*http.Response, error) {
	endpoint, err := validateS3Endpoint(input.Endpoint)
	if err != nil {
		return nil, err
	}
	u := *endpoint
	u.RawQuery = canonicalS3Query(query)
	basePath := strings.TrimRight(u.Path, "/")
	if bucket != "" && !input.PathStyle {
		u.Host = bucket + "." + u.Host
	}
	if bucket != "" && input.PathStyle {
		basePath += "/" + bucket
	}
	if key != "" {
		basePath += "/" + key
	}
	if basePath == "" {
		basePath = "/"
	}
	u.Path = basePath
	u.RawPath = ""

	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if input.Range != "" {
		request.Header.Set("Range", input.Range)
	}
	signS3Request(request, input)
	return p.client.Do(request)
}

func validateS3ProxyRequest(request s3ProxyRequest) error {
	if request.Region == "" || request.AccessKeyID == "" || request.SecretAccessKey == "" {
		return errors.New("region and S3 credentials are required")
	}
	if _, err := validateS3Endpoint(request.Endpoint); err != nil {
		return err
	}
	if request.Bucket != "" && !validProxyBucketName(request.Bucket) {
		return errors.New("invalid bucket")
	}
	if len(request.Key) > 4096 || strings.ContainsAny(request.Key, "\\") {
		return errors.New("invalid object key")
	}
	if len(request.Range) > 128 {
		return errors.New("invalid range")
	}
	for _, pair := range request.Query {
		if len(pair) != 2 || len(pair[0]) > 64 || len(pair[1]) > 4096 {
			return errors.New("invalid query")
		}
	}
	return nil
}

func validProxyBucketName(bucket string) bool {
	if len(bucket) < 1 || len(bucket) > 255 {
		return false
	}
	for _, char := range bucket {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '.' && char != '_' {
			return false
		}
	}
	return true
}

func validateS3Endpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid S3 endpoint")
	}
	if u.Scheme != "https" {
		return nil, errors.New("S3 proxy endpoint must use https")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && blockedProxyIP(ip) {
		return nil, errors.New("private or loopback S3 endpoints are not allowed through the server proxy")
	}
	return u, nil
}

func signS3Request(request *http.Request, input s3ProxyRequest) {
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	shortDate := timestamp[:8]
	const payloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	request.Header.Set("X-Amz-Date", timestamp)
	if input.SessionToken != "" {
		request.Header.Set("X-Amz-Security-Token", input.SessionToken)
	}

	canonicalHeaderNames := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	if input.SessionToken != "" {
		canonicalHeaderNames = append(canonicalHeaderNames, "x-amz-security-token")
	}
	sort.Strings(canonicalHeaderNames)
	var canonicalHeaders strings.Builder
	for _, name := range canonicalHeaderNames {
		value := request.URL.Host
		if name != "host" {
			value = request.Header.Get(name)
		}
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.Join(strings.Fields(value), " "))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(canonicalHeaderNames, ";")
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalS3URI(request.URL.EscapedPath()),
		request.URL.RawQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	scope := shortDate + "/" + input.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + timestamp + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	dateKey := hmacBytes([]byte("AWS4"+input.SecretAccessKey), []byte(shortDate))
	regionKey := hmacBytes(dateKey, []byte(input.Region))
	serviceKey := hmacBytes(regionKey, []byte("s3"))
	signingKey := hmacBytes(serviceKey, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacBytes(signingKey, []byte(stringToSign)))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+input.AccessKeyID+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	request.Header.Set("Accept-Encoding", "identity")
}

func canonicalS3Query(query [][]string) string {
	values := make([]string, 0, len(query))
	for _, pair := range query {
		if len(pair) != 2 {
			continue
		}
		values = append(values, awsURIEncode(pair[0])+"="+awsURIEncode(pair[1]))
	}
	sort.Strings(values)
	return strings.Join(values, "&")
}

func awsURIEncode(value string) string {
	const hexDigits = "0123456789ABCDEF"
	var encoded strings.Builder
	for _, byteValue := range []byte(value) {
		if (byteValue >= 'A' && byteValue <= 'Z') || (byteValue >= 'a' && byteValue <= 'z') || (byteValue >= '0' && byteValue <= '9') || byteValue == '-' || byteValue == '_' || byteValue == '.' || byteValue == '~' {
			encoded.WriteByte(byteValue)
			continue
		}
		encoded.WriteByte('%')
		encoded.WriteByte(hexDigits[byteValue>>4])
		encoded.WriteByte(hexDigits[byteValue&0x0f])
	}
	return encoded.String()
}

func canonicalS3URI(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func hmacBytes(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func safeS3Transport() *http.Transport {
	return &http.Transport{
		// Do not honor process-wide proxy settings here: an HTTP proxy could
		// turn the endpoint field into an SSRF bypass around the DNS/IP checks.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.LookupIP(host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if blockedProxyIP(ip) {
					return nil, errors.New("S3 endpoint resolved to a private or loopback address")
				}
			}
			dialer := net.Dialer{Timeout: 15 * time.Second}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
}

func blockedProxyIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()
}

func setVaultProxyHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
}

func copyS3ProxyHeaders(dst http.ResponseWriter, src http.Header) {
	for _, name := range []string{
		"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified",
		"X-Amz-Meta-S3zero-Iv", "X-Amz-Meta-S3zero-Keyhash", "X-S3Zero-IV", "X-S3Zero-KeyHash",
	} {
		for _, value := range src.Values(name) {
			dst.Header().Add(name, value)
		}
	}
	for source, target := range map[string]string{
		"x-amz-meta-s3zero-iv":      "X-S3Zero-IV",
		"x-amz-meta-s3zero-keyhash": "X-S3Zero-KeyHash",
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
	if dst.Header().Get("Content-Length") == "" {
		if contentLength := src.Get("Content-Length"); contentLength != "" {
			if _, err := strconv.ParseInt(contentLength, 10, 64); err == nil {
				dst.Header().Set("Content-Length", contentLength)
			}
		}
	}
}
