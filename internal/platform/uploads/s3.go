package uploads

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

// S3Config configures a path-style S3-compatible quarantine bucket. Endpoint
// and credentials are operator-owned and are never derived from upload input.
type S3Config struct {
	Endpoint, Bucket, Region, AccessKey, SecretKey string
	Timeout                                        time.Duration
	MaxBytes                                       int64
}

// S3QuarantineStore stores immutable untrusted bytes under tenant-derived keys.
type S3QuarantineStore struct {
	config S3Config
	client *http.Client
	now    func() time.Time
}

// NewS3QuarantineStore creates a SigV4-authenticated S3-compatible adapter.
func NewS3QuarantineStore(config S3Config) (*S3QuarantineStore, error) {
	client := &http.Client{Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return newS3QuarantineStore(config, client, time.Now)
}

func newS3QuarantineStore(config S3Config, client *http.Client, now func() time.Time) (*S3QuarantineStore, error) {
	parsed, err := url.Parse(config.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || config.Bucket == "" || config.Region == "" || config.AccessKey == "" || config.SecretKey == "" || config.Timeout <= 0 || config.Timeout > 2*time.Minute || client == nil || now == nil {
		return nil, ErrInvalid
	}
	return &S3QuarantineStore{config: config, client: client, now: now}, nil
}

// PutQuarantined buffers at most maxBytes so the payload digest can be bound
// into AWS Signature V4 before the request is sent. The API edge maximum keeps
// this bounded; oversized input fails before any object-store mutation.
func (store *S3QuarantineStore) PutQuarantined(ctx context.Context, scope tenancy.Scope, id ID, source io.Reader, maxBytes int64) (StoredObject, error) {
	if ctx == nil || store == nil || source == nil || !scope.Valid() || !id.Valid() || maxBytes < 1 {
		return StoredObject{}, ErrInvalid
	}
	payload, err := io.ReadAll(io.LimitReader(source, maxBytes+1))
	if err != nil || int64(len(payload)) > maxBytes {
		return StoredObject{}, ErrStorage
	}
	digest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(digest[:])
	key := QuarantineObjectKey(scope, id)
	requestURL := strings.TrimRight(store.config.Endpoint, "/") + "/" + escapeS3Segment(store.config.Bucket) + "/" + escapeS3Key(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, bytes.NewReader(payload))
	if err != nil {
		return StoredObject{}, ErrStorage
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	store.sign(request, payloadHash, store.now().UTC())
	response, err := store.client.Do(request)
	if err != nil {
		return StoredObject{}, ErrStorage
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StoredObject{}, ErrStorage
	}
	return StoredObject{Key: key, SizeBytes: int64(len(payload)), SHA256: payloadHash}, nil
}

func (store *S3QuarantineStore) sign(request *http.Request, payloadHash string, now time.Time) {
	amzDate, shortDate := now.Format("20060102T150405Z"), now.Format("20060102")
	request.Header.Set("X-Amz-Date", amzDate)
	canonicalHeaders := "content-type:application/octet-stream\n" + "host:" + request.URL.Host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + amzDate + "\n"
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := request.Method + "\n" + request.URL.EscapedPath() + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash
	canonicalDigest := sha256.Sum256([]byte(canonicalRequest))
	scope := shortDate + "/" + store.config.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalDigest[:])
	dateKey := hmacSHA256([]byte("AWS4"+store.config.SecretKey), shortDate)
	regionKey := hmacSHA256(dateKey, store.config.Region)
	serviceKey := hmacSHA256(regionKey, "s3")
	signingKey := hmacSHA256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+store.config.AccessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func escapeS3Segment(value string) string { return url.PathEscape(value) }

func escapeS3Key(key string) string {
	parts := strings.Split(key, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

type memoryQuarantinedObject struct{ *bytes.Reader }

func (memoryQuarantinedObject) Close() error { return nil }

// OpenQuarantined reads only the server-derived immutable quarantine key and
// keeps the object bounded by the configured upload limit.
func (store *S3QuarantineStore) OpenQuarantined(ctx context.Context, scope tenancy.Scope, id ID, key string) (QuarantinedObject, error) {
	if ctx == nil || store == nil || !scope.Valid() || !id.Valid() || key != QuarantineObjectKey(scope, id) || store.config.MaxBytes < 1 {
		return nil, ErrInvalid
	}
	requestURL := strings.TrimRight(store.config.Endpoint, "/") + "/" + escapeS3Segment(store.config.Bucket) + "/" + escapeS3Key(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, ErrStorage
	}
	empty := sha256.Sum256(nil)
	payloadHash := hex.EncodeToString(empty[:])
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	store.sign(request, payloadHash, store.now().UTC())
	response, err := store.client.Do(request)
	if err != nil {
		return nil, ErrStorage
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, ErrStorage
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, store.config.MaxBytes+1))
	if err != nil || int64(len(payload)) > store.config.MaxBytes {
		return nil, ErrStorage
	}
	return memoryQuarantinedObject{Reader: bytes.NewReader(payload)}, nil
}

// Promote verifies the quarantine digest before writing the same bytes to the
// server-derived released key. The quarantine copy is retained as immutable
// security evidence; downstream consumers can only resolve the released key.
func (store *S3QuarantineStore) Promote(ctx context.Context, scope tenancy.Scope, id ID, fromKey, expectedSHA256 string) (StoredObject, error) {
	if ctx == nil || store == nil || fromKey != QuarantineObjectKey(scope, id) || len(expectedSHA256) != 64 || store.config.MaxBytes < 1 {
		return StoredObject{}, ErrInvalid
	}
	object, err := store.OpenQuarantined(ctx, scope, id, fromKey)
	if err != nil {
		return StoredObject{}, err
	}
	defer object.Close()
	payload, err := io.ReadAll(io.LimitReader(object, store.config.MaxBytes+1))
	if err != nil || int64(len(payload)) > store.config.MaxBytes {
		return StoredObject{}, ErrStorage
	}
	digest := sha256.Sum256(payload)
	actual := hex.EncodeToString(digest[:])
	if actual != expectedSHA256 {
		return StoredObject{}, ErrStorage
	}
	key := ReleasedObjectKey(scope, id)
	requestURL := strings.TrimRight(store.config.Endpoint, "/") + "/" + escapeS3Segment(store.config.Bucket) + "/" + escapeS3Key(key)
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, bytes.NewReader(payload))
	if err != nil {
		return StoredObject{}, ErrStorage
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Amz-Content-Sha256", actual)
	store.sign(request, actual, store.now().UTC())
	response, err := store.client.Do(request)
	if err != nil {
		return StoredObject{}, ErrStorage
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return StoredObject{}, ErrStorage
	}
	return StoredObject{Key: key, SizeBytes: int64(len(payload)), SHA256: actual}, nil
}

var _ QuarantineStore = (*S3QuarantineStore)(nil)
var _ QuarantineReader = (*S3QuarantineStore)(nil)
var _ ReleaseStore = (*S3QuarantineStore)(nil)
