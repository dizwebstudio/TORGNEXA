package builtinruntime

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/netip"
	"os"
	"testing"
	"time"

	maxmessenger "github.com/torgnexa/torgnexa/connectors/social/max-messenger"
	telegram "github.com/torgnexa/torgnexa/connectors/social/telegram"
)

func TestHostAndAddressPolicy(t *testing.T) {
	for _, host := range []string{"api-seller.ozon.ru", "marketplace-api.wildberries.ru", "api.moysklad.ru"} {
		if !validHost(host) {
			t.Fatalf("valid host rejected: %s", host)
		}
	}
	for _, host := range []string{"localhost", "127.0.0.1", "Example.COM", "metadata.local", "http://example.com"} {
		if validHost(host) {
			t.Fatalf("unsafe host accepted: %s", host)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "192.0.2.1", "2001:db8::1"} {
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			t.Fatal(err)
		}
		if publicIP(addr) {
			t.Fatalf("unsafe address accepted: %s", raw)
		}
	}
}

func TestMAXRuntimeRequestAdmissionIsExact(t *testing.T) {
	allowed := []maxmessenger.Request{
		{Method: "GET", Path: "/me"},
		{Method: "GET", Path: "/chats/-70801090403050"},
		{Method: "GET", Path: "/chats/-70801090403050/members/me"},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "-70801090403050"}}, Body: []byte(`{"text":"test"}`)},
		{Method: "POST", Path: "/uploads", Params: []maxmessenger.Param{{Name: "type", Value: "image"}}},
		{Method: "POST", Path: "/uploads", Params: []maxmessenger.Param{{Name: "type", Value: "video"}}},
	}
	for _, request := range allowed {
		if !validMAXRuntimeRequest(request) {
			t.Fatalf("admitted MAX request rejected: %+v", request)
		}
	}
	rejected := []maxmessenger.Request{
		{Method: "GET", Path: "chats/1"},
		{Method: "GET", Path: "/messages/1"},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "0"}}, Body: []byte(`{"text":"test"}`)},
		{Method: "POST", Path: "/messages", Params: []maxmessenger.Param{{Name: "chat_id", Value: "1"}, {Name: "user_id", Value: "2"}}, Body: []byte(`{"text":"test"}`)},
		{Method: "POST", Path: "/uploads", Body: []byte(`{}`)},
		{Method: "POST", Path: "/uploads", Params: []maxmessenger.Param{{Name: "type", Value: "audio"}}},
	}
	for _, request := range rejected {
		if validMAXRuntimeRequest(request) {
			t.Fatalf("unadmitted MAX request accepted: %+v", request)
		}
	}
}

func TestMAXMediaUploadUsesOfficialHostAndBoundedMultipart(t *testing.T) {
	upload := maxmessenger.UploadRequest{URL: "https://omub.okcdn.ru/upload.do?sig=fixture", FileName: "upl_0123456789abcdef0123456789abcdef.mp4", MediaType: "video/mp4", SizeBytes: 5, Body: bytes.NewReader([]byte("video"))}
	body, contentType, err := maxMultipartBody(upload)
	if err != nil {
		t.Fatal(err)
	}
	if !validMAXUploadURL(upload.URL, upload.MediaType) || validMAXUploadURL("https://omub.okcdn.ru.evil.example/upload", upload.MediaType) || validMAXUploadURL("https://omub.okcdn.ru:444/upload", upload.MediaType) {
		t.Fatal("unsafe or non-official MAX upload URL accepted")
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("invalid multipart content type: %q: %v", contentType, err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	part, err := reader.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := io.ReadAll(part)
	if err != nil || string(payload) != "video" || part.FormName() != "data" || part.FileName() != upload.FileName {
		t.Fatalf("unexpected upload part: name=%q file=%q body=%q err=%v", part.FormName(), part.FileName(), payload, err)
	}
}

func TestTelegramMultipartBodyPreservesFieldsAndFile(t *testing.T) {
	body, contentType, err := telegramMultipartBody(
		[]telegram.Param{{Name: "chat_id", Value: "-1001234567890"}, {Name: "caption", Value: "hello"}},
		[]telegram.FilePart{{FieldName: "photo0", FileName: "upl_0123456789abcdef0123456789abcdef.jpg", MediaType: "image/jpeg", SizeBytes: 5, Body: bytes.NewReader([]byte("photo"))}},
	)
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("invalid multipart content type: %q: %v", contentType, err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	fields := map[string]string{}
	var file []byte
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		payload, readErr := io.ReadAll(part)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if part.FormName() == "photo0" {
			file = payload
			if part.FileName() != "upl_0123456789abcdef0123456789abcdef.jpg" || part.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("invalid file part: %+v", part.Header)
			}
			continue
		}
		fields[part.FormName()] = string(payload)
	}
	if fields["chat_id"] != "-1001234567890" || fields["caption"] != "hello" || string(file) != "photo" {
		t.Fatalf("multipart payload was not preserved: fields=%v file=%q", fields, file)
	}
}

func TestTelegramMultipartBodyRejectsSizeMismatchAndUnsafeMedia(t *testing.T) {
	if _, _, err := telegramMultipartBody(nil, []telegram.FilePart{{FieldName: "photo0", FileName: "x.jpg", MediaType: "image/jpeg", SizeBytes: 4, Body: bytes.NewReader([]byte("abc"))}}); err == nil {
		t.Fatal("short file was accepted")
	}
	if _, _, err := telegramMultipartBody(nil, []telegram.FilePart{{FieldName: "photo0", FileName: "x.svg", MediaType: "image/svg+xml", SizeBytes: 3, Body: bytes.NewReader([]byte("abc"))}}); err == nil {
		t.Fatal("unsupported media type was accepted")
	}
}

func TestLiveCBRDailyTransport(t *testing.T) {
	if os.Getenv("TORGNEXA_LIVE_CBR") != "1" {
		t.Skip("live CBR qualification disabled")
	}
	body, err := newCBRDailyHTTP(newHTTPTransport()).Daily(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 1024 {
		t.Fatalf("CBR daily document unexpectedly small: %d", len(body))
	}
}
