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

func TestClientRejectsRedirects(t *testing.T) {
	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target was contacted")
	}))
	defer redirected.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirected.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	_, err := NewClient(upstream.URL, "test-secret").Version(context.Background())
	var upstreamErr *UpstreamError
	if !errors.As(err, &upstreamErr) || upstreamErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect returned %v", err)
	}
}
