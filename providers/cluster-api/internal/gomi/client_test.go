package gomi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerValidation(t *testing.T) {
	for _, s := range []string{"", "file:///tmp/secret", "https://user:password@example.com", "https://example.com/api/v1", "https://example.com?token=x", "https://example.com#x"} {
		if _, err := New(s, "secret"); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
	if _, err := New("https://example.com", ""); err == nil {
		t.Fatal("empty token accepted")
	}
}
func TestErrorRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("bootstrap-token SECRET"))
	}))
	defer srv.Close()
	c, _ := New(srv.URL, "SECRET")
	_, err := c.GetVM(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "SECRET") || !IsStatus(err, 500) {
		t.Fatalf("unsafe error: %v", err)
	}
}
func TestRedirectRefused(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer srv.Close()
	c, _ := New(srv.URL, "SECRET")
	_, err := c.GetVM(context.Background(), "x")
	if !IsStatus(err, 302) || called {
		t.Fatal("followed credential-bearing redirect")
	}
}
func TestContextCancellation(t *testing.T) {
	c, _ := New("http://127.0.0.1:1", "token")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetVM(ctx, "x"); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestVMOwnership(t *testing.T) {
	for _, tt := range []struct {
		name  string
		vm    VM
		owned bool
	}{
		{"legacy", VM{CloudInitRef: "cloudInitTemplates/capi-a"}, true},
		{"canonical", VM{CloudInitRefs: []string{"cloudInitTemplates/capi-a"}}, true},
		{"multiple", VM{CloudInitRefs: []string{"capi-a", "foreign"}}, false},
		{"foreign-array-wins", VM{CloudInitRef: "capi-a", CloudInitRefs: []string{"foreign"}}, false},
		{"empty", VM{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.vm.OwnedBy("capi-a") != tt.owned {
				t.Fatal("incorrect ownership decision")
			}
		})
	}
}
