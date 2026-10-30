package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func testConfig(t *testing.T, upstreamURL string) (config, *url.URL) {
	t.Helper()
	cfg := config{
		socket:      filepath.Join(t.TempDir(), "app.sock"),
		upstream:    upstreamURL,
		prefix:      "/app/magpie",
		cookieName:  "magpie_web_3430",
		cookieValue: "0123456789abcdef0123456789abcdef",
		authHeader:  "X-Trim-Userid",
		shimEnabled: true,
	}
	u, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatalf("parse upstream: %v", err)
	}
	return cfg, u
}

func TestRedirectsBarePrefix(t *testing.T) {
	called := false
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer up.Close()
	cfg, u := testConfig(t, up.URL)
	h := newBridge(cfg, u)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/magpie", nil))
	if rec.Code != http.StatusPermanentRedirect {
		t.Fatalf("bare prefix: got %d, want 308", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/app/magpie/" {
		t.Fatalf("Location: got %q, want /app/magpie/", loc)
	}
	if called {
		t.Fatal("upstream must not be called for the redirect")
	}
}

func TestStripsPrefixAndInjectsCookie(t *testing.T) {
	var gotPath, gotCookie string
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if c, err := r.Cookie("magpie_web_3430"); err == nil {
			gotCookie = c.Value
		}
	}))
	defer up.Close()
	cfg, u := testConfig(t, up.URL)
	h := newBridge(cfg, u)

	req := httptest.NewRequest("GET", "/app/magpie/api/state", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: got %d", rec.Code)
	}
	if gotPath != "/api/state" {
		t.Fatalf("upstream path: got %q, want /api/state", gotPath)
	}
	if gotCookie != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("cookie: got %q", gotCookie)
	}
}

func TestNoCookieWithoutAuthHeader(t *testing.T) {
	var gotCookie string
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("magpie_web_3430"); err == nil {
			gotCookie = c.Value
		}
	}))
	defer up.Close()
	cfg, u := testConfig(t, up.URL)
	h := newBridge(cfg, u)

	req := httptest.NewRequest("GET", "/app/magpie/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if gotCookie != "" {
		t.Fatalf("cookie injected without the auth header: %q", gotCookie)
	}
}

func TestRejectsForeignPaths(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
	defer up.Close()
	cfg, u := testConfig(t, up.URL)
	h := newBridge(cfg, u)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/elsewhere/secret", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign path: got %d, want 404", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("CONNECT", "/app/magpie/x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("CONNECT: got %d, want 403", rec.Code)
	}
}

func TestInjectsShimOnPageOnly(t *testing.T) {
	page := []byte("<!doctype html><html><head><title>t</title></head><body></body></html>")
	up := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") == "identity" {
			rw.Header().Set("X-Uncompressed", "1")
		}
		rw.Header().Set("Content-Type", "text/html")
		rw.Write(page)
	}))
	defer up.Close()
	cfg, u := testConfig(t, up.URL)
	h := newBridge(cfg, u)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/magpie/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "window.fetch") || !strings.Contains(body, `"/app/magpie"`) && !strings.Contains(body, "location.pathname") {
		t.Fatal("shim not injected into the page")
	}
	if rec.Header().Get("Content-Length") != strconv.Itoa(len(body)) {
		t.Fatalf("content length not updated: %s vs %d", rec.Header().Get("Content-Length"), len(body))
	}
	// a non-page route must pass through untouched
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/magpie/app.js", nil))
	if strings.Contains(rec.Body.String(), "window.fetch=function") {
		t.Fatal("shim must not be injected into non-page routes")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	t.Setenv("BRIDGE_SOCKET", filepath.Join(t.TempDir(), "s.sock"))
	t.Setenv("BRIDGE_PREFIX", "/app/magpie")
	t.Setenv("BRIDGE_COOKIE_NAME", "c")
	t.Setenv("BRIDGE_COOKIE_VALUE", "0123456789abcdef0123456789abcdef")
	t.Setenv("BRIDGE_AUTH_HEADER", "X-Trim-Userid")

	cases := []struct {
		name     string
		upstream string
		wantErr  bool
	}{
		{"https ok", "https://magpie:3430", false},
		{"http ok", "http://magpie:3430", false},
		{"gopher rejected", "gopher://magpie:3430", true},
		{"no host rejected", "http://", true},
		{"credentials rejected", "http://user:pass@magpie:3430", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BRIDGE_UPSTREAM", tc.upstream)
			_, err := loadConfig()
			if (err != nil) != tc.wantErr {
				t.Fatalf("loadConfig(%q): err=%v, wantErr=%v", tc.upstream, err, tc.wantErr)
			}
		})
	}
}
