package mihomo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEscapingAndUpstreamError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/proxies/%E8%8A%82%E7%82%B9%2Fa%20%F0%9F%98%80" {
			t.Errorf("path: %s", r.URL.EscapedPath())
		}
		if r.Header.Get("Authorization") != "Bearer test" {
			t.Error("missing authorization")
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad"))
	}))
	defer s.Close()
	_, err := NewClient(s.URL, "test").Proxy(context.Background(), "节点/a 😀")
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.StatusCode != 400 || string(upstream.Body) != "bad" {
		t.Fatalf("error: %v", err)
	}
}
