package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestS3QuarantineStoreSignsBoundedTenantDerivedPut(t *testing.T) {
	scope := mustScope(t, org, ws)
	id := ID("upl_0123456789abcdef0123456789abcdef")
	payload := []byte("synthetic upload")
	wantDigest := sha256.Sum256(payload)
	now := time.Date(2026, 8, 16, 12, 34, 56, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/tenant-files/"+QuarantineObjectKey(scope, id) {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, payload) {
			t.Errorf("payload = %q", body)
		}
		if r.Header.Get("X-Amz-Date") != "20260816T123456Z" || r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(wantDigest[:]) {
			t.Errorf("signature headers = %#v", r.Header)
		}
		authorization := r.Header.Get("Authorization")
		if !strings.Contains(authorization, "Credential=access/20260816/garage/s3/aws4_request") || !strings.Contains(authorization, "SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date") || strings.Contains(authorization, "secret") {
			t.Errorf("authorization header = %q", authorization)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store, err := newS3QuarantineStore(S3Config{Endpoint: server.URL, Bucket: "tenant-files", Region: "garage", AccessKey: "access", SecretKey: "secret", Timeout: time.Second}, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.PutQuarantined(context.Background(), scope, id, bytes.NewReader(payload), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if object.Key != QuarantineObjectKey(scope, id) || object.SizeBytes != int64(len(payload)) || object.SHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("object = %+v", object)
	}
}

func TestS3QuarantineStoreRejectsOversizeBeforeEgress(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	store, err := NewS3QuarantineStore(S3Config{Endpoint: server.URL, Bucket: "tenant-files", Region: "garage", AccessKey: "access", SecretKey: "secret", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.PutQuarantined(context.Background(), mustScope(t, org, ws), ID("upl_0123456789abcdef0123456789abcdef"), strings.NewReader("too large"), 3)
	if !errors.Is(err, ErrStorage) || calls.Load() != 0 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestS3QuarantineStorePromotesVerifiedQuarantineObject(t *testing.T) {
	scope := mustScope(t, org, ws)
	id := ID("upl_0123456789abcdef0123456789abcdef")
	payload := []byte("verified quarantine payload")
	digest := sha256.Sum256(payload)
	expectedSHA := hex.EncodeToString(digest[:])
	now := time.Date(2026, 8, 17, 1, 2, 3, 0, time.UTC)
	var getCalls, putCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/tenant-files/"+QuarantineObjectKey(scope, id):
			getCalls.Add(1)
			if r.Header.Get("Authorization") == "" || r.Header.Get("X-Amz-Date") != "20260817T010203Z" {
				t.Errorf("unsigned quarantine GET: %#v", r.Header)
			}
			_, _ = w.Write(payload)
		case r.Method == http.MethodPut && r.URL.Path == "/tenant-files/"+ReleasedObjectKey(scope, id):
			putCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, payload) {
				t.Errorf("released payload = %q", body)
			}
			if r.Header.Get("X-Amz-Content-Sha256") != expectedSHA || r.Header.Get("Authorization") == "" {
				t.Errorf("unsigned released PUT: %#v", r.Header)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	store, err := newS3QuarantineStore(S3Config{
		Endpoint: server.URL, Bucket: "tenant-files", Region: "garage", AccessKey: "access", SecretKey: "secret", Timeout: time.Second, MaxBytes: 1024,
	}, server.Client(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	object, err := store.Promote(context.Background(), scope, id, QuarantineObjectKey(scope, id), expectedSHA)
	if err != nil {
		t.Fatal(err)
	}
	if object.Key != ReleasedObjectKey(scope, id) || object.SHA256 != expectedSHA || object.SizeBytes != int64(len(payload)) {
		t.Fatalf("released object = %+v", object)
	}
	if getCalls.Load() != 1 || putCalls.Load() != 1 {
		t.Fatalf("get=%d put=%d", getCalls.Load(), putCalls.Load())
	}
}

func TestS3QuarantineStoreRefusesDigestMismatchBeforeReleasePut(t *testing.T) {
	scope := mustScope(t, org, ws)
	id := ID("upl_0123456789abcdef0123456789abcdef")
	payload := []byte("quarantine payload")
	var putCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write(payload)
			return
		}
		if r.Method == http.MethodPut {
			putCalls.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	store, err := newS3QuarantineStore(S3Config{
		Endpoint: server.URL, Bucket: "tenant-files", Region: "garage", AccessKey: "access", SecretKey: "secret", Timeout: time.Second, MaxBytes: 1024,
	}, server.Client(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Promote(context.Background(), scope, id, QuarantineObjectKey(scope, id), strings.Repeat("0", 64))
	if !errors.Is(err, ErrStorage) || putCalls.Load() != 0 {
		t.Fatalf("err=%v put_calls=%d", err, putCalls.Load())
	}
}
