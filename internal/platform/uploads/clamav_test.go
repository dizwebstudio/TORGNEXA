package uploads

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type oneShotDialer struct{ conn net.Conn }

func (d oneShotDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	return d.conn, nil
}

func TestClamAVScannerStreamsBoundedINSTREAM(t *testing.T) {
	client, server := net.Pipe()
	data := []byte("synthetic clean payload")
	done := make(chan []byte, 1)
	go func() {
		defer server.Close()
		r := bufio.NewReader(server)
		cmd := make([]byte, len("zINSTREAM\x00"))
		if _, err := io.ReadFull(r, cmd); err != nil {
			return
		}
		var got bytes.Buffer
		for {
			var header [4]byte
			if _, err := io.ReadFull(r, header[:]); err != nil {
				return
			}
			n := binary.BigEndian.Uint32(header[:])
			if n == 0 {
				break
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(r, buf); err != nil {
				return
			}
			got.Write(buf)
		}
		done <- got.Bytes()
		_, _ = server.Write([]byte("stream: OK\x00"))
	}()
	cfg := ClamAVConfig{Network: "tcp", Address: "clamd.internal:3310", EngineVersion: "1.4.3", SignatureVersion: "daily-20260810", Timeout: time.Second, MaxBytes: 1024}
	scanner, err := newClamAVScanner(cfg, oneShotDialer{client})
	if err != nil {
		t.Fatal(err)
	}
	res, err := scanner.Scan(context.Background(), ScanRequest{UploadID: ID("upl_102132435465768798a9bacbdcedfe0f"), SHA256: sha256Hex(data), SizeBytes: int64(len(data)), MediaType: "text/plain", Policy: "upload-security-v1", ContentType: "text/plain"}, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ScannerClean || res.ScannerName != "clamav" {
		t.Fatalf("res=%+v", res)
	}
	select {
	case got := <-done:
		if !bytes.Equal(got, data) {
			t.Fatalf("got=%q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive stream")
	}
}

func TestClamAVResponseHashesThreatNameAndFailsClosedOnErrors(t *testing.T) {
	cfg := ClamAVConfig{Network: "unix", Address: "/run/clamav/clamd.sock", EngineVersion: "1.4.3", SignatureVersion: "daily-20260810", Timeout: time.Second, MaxBytes: 1024}
	infected, err := parseClamAVResponse("stream: SYNTHETIC-TEST-SIGNATURE FOUND\x00", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if infected.Status != ScannerInfected || infected.ThreatCode == "" || infected.ThreatCode == "synthetic-test-signature" {
		t.Fatalf("infected=%+v", infected)
	}
	if _, err := parseClamAVResponse("stream: temporary scanner failure ERROR\x00", cfg); !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if _, err := parseClamAVResponse("unexpected: OK\x00", cfg); !errors.Is(err, ErrScannerUnavailable) {
		t.Fatalf("malformed clean response must fail closed: %v", err)
	}
}
