package mcp

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func nil2() *http.Client { return &http.Client{Timeout: 10 * time.Second} }

func listenLoopback() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }

func hit(t *testing.T, u string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Error(err)
		return
	}
	resp.Body.Close()
}
