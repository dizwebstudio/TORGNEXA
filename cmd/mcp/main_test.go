package main

import (
	"net"
	"testing"
)

func TestHealthcheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := healthcheck(ln.Addr().String()); err != nil {
		t.Fatalf("healthcheck: %v", err)
	}
}
