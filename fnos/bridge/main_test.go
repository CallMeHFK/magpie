package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
		adminHeader: "X-Trim-Isadmin",
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
	req.Header.Set("X-Trim-Isadmin", "true")
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

// The gateway vouches only that a session is signed in; the console changes
// every account and gateway key, so the web key cookie is for an fnOS
// administrator alone. A non-admin still gets the page - magpie's sign-in
// form - but never the key.
func TestCookieOnlyForAdmin(t *testing.T) {
	cases := []struct {
		name    string
		user    string
		admin   string
		wantKey bool
	}{
		{"admin", "1000", "true", true},
		{"admin upper case", "1000", "TRUE", true},
		{"admin with padding", "1000", " true ", true},
		{"not admin", "1000", "false", false},
		{"no admin header", "1000", "", false},
		{"garbage admin header", "1000", "yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
			req.Header.Set("X-Trim-Userid", tc.user)
			if tc.admin != "" {
				req.Header.Set("X-Trim-Isadmin", tc.admin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("status: got %d, want the page for every signed-in user", rec.Code)
			}
			if tc.wantKey && gotCookie != "0123456789abcdef0123456789abcdef" {
				t.Fatalf("cookie: got %q, want the web key for an administrator", gotCookie)
			}
			if !tc.wantKey && gotCookie != "" {
				t.Fatalf("cookie: got %q, want none for a non-administrator", gotCookie)
			}
		})
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
	t.Setenv("BRIDGE_KEY_FILE", filepath.Join(t.TempDir(), "magpie.env"))
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

func TestKeyFileRequired(t *testing.T) {
	t.Setenv("BRIDGE_SOCKET", filepath.Join(t.TempDir(), "s.sock"))
	t.Setenv("BRIDGE_PREFIX", "/app/magpie")
	t.Setenv("BRIDGE_UPSTREAM", "http://magpie:3430")
	t.Setenv("BRIDGE_KEY_FILE", "")
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig without BRIDGE_KEY_FILE: got nil error, want the required-path error")
	}
}

func TestCookieValueFallsBackToWebKey(t *testing.T) {
	t.Setenv("BRIDGE_SOCKET", filepath.Join(t.TempDir(), "s.sock"))
	t.Setenv("BRIDGE_KEY_FILE", filepath.Join(t.TempDir(), "magpie.env"))
	t.Setenv("BRIDGE_UPSTREAM", "http://magpie:3430")
	t.Setenv("BRIDGE_PREFIX", "/app/magpie")
	t.Setenv("BRIDGE_COOKIE_NAME", "c")
	t.Setenv("BRIDGE_AUTH_HEADER", "X-Trim-Userid")
	t.Setenv("MAGPIE_WEB_KEY", "fedcba9876543210fedcba9876543210")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.cookieValue != "fedcba9876543210fedcba9876543210" {
		t.Fatalf("cookie value: got %q, want the MAGPIE_WEB_KEY fallback", cfg.cookieValue)
	}

	t.Setenv("BRIDGE_COOKIE_VALUE", "0123456789abcdef0123456789abcdef")
	cfg, err = loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.cookieValue != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("cookie value: got %q, want the BRIDGE_COOKIE_VALUE override", cfg.cookieValue)
	}
}

// Without an owner the key file keeps the plain mode the nonroot magpie
// container reads through the shared mount.
func TestKeyFileKeepsItsModeWithoutOwner(t *testing.T) {
	// a tight umask must not turn the mode the reader needs into something
	// else; the bridge sets it rather than inheriting it
	defer syscall.Umask(syscall.Umask(0o077))
	path := filepath.Join(t.TempDir(), "magpie.env")
	key, err := adoptKeyFile(path, nil)
	if err != nil {
		t.Fatalf("adoptKeyFile: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key %q: want 32 hex characters", key)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o644 {
		t.Fatalf("mode without an owner: got %o, want 644", got)
	}
}

// With an owner the key is theirs alone, including a file an older bridge
// wrote world readable.
func TestKeyFileIsGivenToItsOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("giving the key file to another uid needs root; run this test as root")
	}
	path := filepath.Join(t.TempDir(), "magpie.env")
	if err := os.WriteFile(path, []byte("MAGPIE_WEB_KEY=0123456789abcdef0123456789abcdef\n"), 0o644); err != nil {
		t.Fatalf("seed a world-readable key: %v", err)
	}
	owner := &keyOwner{uid: 65532, gid: 65532}
	key, err := adoptKeyFile(path, owner)
	if err != nil {
		t.Fatalf("adoptKeyFile: %v", err)
	}
	if key != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("key: got %q, want the file's existing key", key)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode with an owner: got %o, want 600", got)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("stat: no Stat_t, got %T", fi.Sys())
	}
	if int(st.Uid) != owner.uid || int(st.Gid) != owner.gid {
		t.Fatalf("owner: got %d:%d, want %d:%d", st.Uid, st.Gid, owner.uid, owner.gid)
	}
}

func TestLoadKeyOwner(t *testing.T) {
	cases := []struct {
		name    string
		uid     string
		gid     string
		wantNil bool
		wantErr bool
	}{
		{"neither", "", "", true, false},
		{"uid alone", "65532", "", false, true},
		{"gid alone", "", "65532", false, true},
		{"uid not a number", "magpie", "65532", false, true},
		{"gid not a number", "65532", "magpie", false, true},
		{"negative", "-1", "65532", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BRIDGE_KEY_UID", tc.uid)
			t.Setenv("BRIDGE_KEY_GID", tc.gid)
			owner, err := loadKeyOwner()
			if (err != nil) != tc.wantErr {
				t.Fatalf("loadKeyOwner(%q, %q): err=%v, wantErr=%v", tc.uid, tc.gid, err, tc.wantErr)
			}
			if tc.wantNil && owner != nil {
				t.Fatalf("loadKeyOwner(%q, %q): got %+v, want nil", tc.uid, tc.gid, owner)
			}
		})
	}

	// both set and well formed: accepted only where the chown can happen
	t.Setenv("BRIDGE_KEY_UID", "65532")
	t.Setenv("BRIDGE_KEY_GID", "65532")
	owner, err := loadKeyOwner()
	if os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("loadKeyOwner as root: %v", err)
		}
		if owner == nil || owner.uid != 65532 || owner.gid != 65532 {
			t.Fatalf("owner: got %+v, want 65532:65532", owner)
		}
	} else if err == nil {
		t.Fatal("loadKeyOwner without root: got nil error, want the root requirement")
	}
}
