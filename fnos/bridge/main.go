// Command bridge is the fnOS unified-gateway adapter for the magpie app
// package. The fnOS gateway authenticates a session, then forwards
// /app/magpie/* to a Unix socket this process listens on; the bridge strips
// the gateway prefix, hands an fnOS administrator's browser magpie's web
// key as a cookie, and proxies everything to magpie web's one-port server (the
// page, its /api/* and the gateway client routes /v1, /v1beta, /mcp on the
// same port).
//
// The upstream address is fixed at startup from the environment (the app's
// compose file); it is never taken from a request, so there is no
// request-driven egress: every byte goes to that one upstream and nowhere
// else.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type config struct {
	socket      string    // BRIDGE_SOCKET, the Unix socket to listen on
	upstream    string    // BRIDGE_UPSTREAM, the fixed http(s) URL to proxy to
	prefix      string    // BRIDGE_PREFIX, the gateway path prefix to strip
	cookieName  string    // BRIDGE_COOKIE_NAME, magpie web's key cookie
	cookieValue string    // BRIDGE_COOKIE_VALUE, MAGPIE_WEB_KEY
	authHeader  string    // BRIDGE_AUTH_HEADER, the fnOS gateway user header
	adminHeader string    // BRIDGE_ADMIN_HEADER, the gateway's administrator flag
	keyFile     string    // BRIDGE_KEY_FILE, the key file adopted when no env key is set
	keyOwner    *keyOwner // BRIDGE_KEY_UID/BRIDGE_KEY_GID, owner of a key file this process writes
	shimEnabled bool      // BRIDGE_FETCH_SHIM=1, rewrite absolute fetch URLs
}

// keyOwner is who a generated key file belongs to. The bridge runs as root
// in the packaged compose file, but the magpie container reads the key as an
// unprivileged user; handing the file to that user with 0600 is what keeps
// it unreadable to everything else on the host.
type keyOwner struct {
	uid int
	gid int
}

func loadConfig() (config, error) {
	var c config
	var err error
	for _, e := range []struct {
		name string
		dst  *string
	}{
		{"BRIDGE_SOCKET", &c.socket},
		{"BRIDGE_UPSTREAM", &c.upstream},
		{"BRIDGE_PREFIX", &c.prefix},
		{"BRIDGE_COOKIE_NAME", &c.cookieName},
	} {
		v := os.Getenv(e.name)
		if v == "" {
			return c, fmt.Errorf("bridge: %s is required", e.name)
		}
		*e.dst = v
	}
	// The fnOS gateway names the authenticated user in this header; a
	// deployment may override it, but this is the documented default.
	c.authHeader = os.Getenv("BRIDGE_AUTH_HEADER")
	if c.authHeader == "" {
		c.authHeader = "X-Trim-Userid"
	}
	// The gateway says whether that user is an administrator in this one; it
	// is forwarded for every session, so a missing value means "no".
	c.adminHeader = os.Getenv("BRIDGE_ADMIN_HEADER")
	if c.adminHeader == "" {
		c.adminHeader = "X-Trim-Isadmin"
	}
	if c.keyOwner, err = loadKeyOwner(); err != nil {
		return c, err
	}
	// The web key comes in as MAGPIE_WEB_KEY or BRIDGE_COOKIE_VALUE (the
	// magpie container sources the same key file); when neither is set, the
	// bridge adopts BRIDGE_KEY_FILE: it reads the file, or generates it, so
	// the magpie side (whose entrypoint waits for the file) serves with the
	// very same key. Without any key the bridge still runs - it just stops
	// injecting the cookie, and magpie web asks visitors for its key.
	c.cookieValue = os.Getenv("BRIDGE_COOKIE_VALUE")
	if c.cookieValue == "" {
		c.cookieValue = os.Getenv("MAGPIE_WEB_KEY")
	}
	c.keyFile = os.Getenv("BRIDGE_KEY_FILE")
	if c.cookieValue == "" && c.keyFile != "" {
		if v, err := adoptKeyFile(c.keyFile, c.keyOwner); err != nil {
			log.Printf("bridge: key file %s: %v (continuing without cookie injection)", c.keyFile, err)
		} else if v != "" {
			c.cookieValue = v
		}
	}
	if c.cookieValue != "" && len(c.cookieValue) < 16 {
		return c, fmt.Errorf("bridge: the web key is shorter than 16 characters")
	}
	// The upstream is operator configuration, not request data: accept only
	// plain http or https, never a file/gopher/etc scheme, and refuse
	// credentials embedded in the URL.
	u, err := url.Parse(c.upstream)
	if err != nil {
		return c, fmt.Errorf("bridge: BRIDGE_UPSTREAM %q: %w", c.upstream, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return c, fmt.Errorf("bridge: BRIDGE_UPSTREAM must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return c, fmt.Errorf("bridge: BRIDGE_UPSTREAM %q has no host", c.upstream)
	}
	if u.User != nil {
		return c, fmt.Errorf("bridge: BRIDGE_UPSTREAM must not carry credentials")
	}
	if !strings.HasPrefix(c.prefix, "/app/") || strings.HasSuffix(c.prefix, "/") {
		return c, fmt.Errorf("bridge: BRIDGE_PREFIX must be /app/<appname>, got %q", c.prefix)
	}
	// Both paths are operator configuration (the app's compose file), but a
	// malformed deployment should fail loudly at startup: the socket lives
	// inside the app's own mounted directory, and the key file is written
	// next to it - neither may escape that location via a relative or
	// non-normalized path.
	if _, err := cleanBridgePath("BRIDGE_SOCKET", c.socket); err != nil {
		return c, err
	}
	if _, err := cleanBridgePath("BRIDGE_KEY_FILE", c.keyFile); err != nil {
		return c, err
	}
	c.shimEnabled = os.Getenv("BRIDGE_FETCH_SHIM") == "1"
	return c, nil
}

// cleanBridgePath validates an operator-provided POSIX filesystem path:
// absolute and normalized (path.Clean is a no-op on it, which rules out
// relative segments, duplicate separators and parent-directory walks).
func cleanBridgePath(kind, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("bridge: %s is required", kind)
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("bridge: %s must be an absolute POSIX path, got %q", kind, p)
	}
	if path.Clean(p) != p {
		return "", fmt.Errorf("bridge: %s must be a normalized path, got %q", kind, p)
	}
	return p, nil
}

// loadKeyOwner reads BRIDGE_KEY_UID and BRIDGE_KEY_GID, the owner a key file
// this process writes is handed to. They go together; without them the file
// keeps the uid this process runs as and is world readable (0644), which only
// works where the mount is private to the two containers.
func loadKeyOwner() (*keyOwner, error) {
	uid, gid := os.Getenv("BRIDGE_KEY_UID"), os.Getenv("BRIDGE_KEY_GID")
	if uid == "" && gid == "" {
		return nil, nil
	}
	if uid == "" || gid == "" {
		return nil, errors.New("bridge: BRIDGE_KEY_UID and BRIDGE_KEY_GID go together")
	}
	o := &keyOwner{}
	var err error
	if o.uid, err = strconv.Atoi(uid); err != nil || o.uid < 0 {
		return nil, fmt.Errorf("bridge: BRIDGE_KEY_UID %q is not a user id", uid)
	}
	if o.gid, err = strconv.Atoi(gid); err != nil || o.gid < 0 {
		return nil, fmt.Errorf("bridge: BRIDGE_KEY_GID %q is not a group id", gid)
	}
	// giving the file away is a privileged operation; saying so at startup
	// beats a key the reader cannot open, with only a log line to show it
	if os.Geteuid() != 0 {
		return nil, errors.New("bridge: BRIDGE_KEY_UID needs the bridge to run as root")
	}
	return o, nil
}

// adoptKeyFile reads a KEY=VALUE env file and returns MAGPIE_WEB_KEY from
// it; when the file is missing it is created with a fresh random key. The
// file lives on the app's own directory, whose perms the fnOS gateway host
// sets, and it is readable only by the user it is handed to (owner) so no
// other app on the device can read the key. Without an owner - a deployment
// where only the two containers share the mount - it stays 0644 for the
// nonroot magpie container to read.
func adoptKeyFile(path string, owner *keyOwner) (string, error) {
	mode := os.FileMode(0o644)
	if owner != nil {
		mode = 0o600
	}
	key, err := readOrCreateKeyFile(path, mode)
	if err != nil {
		return "", err
	}
	if owner != nil {
		// an older bridge wrote 0644; take it down on the next start
		if err := os.Chmod(path, 0o600); err != nil {
			return "", err
		}
		if err := os.Chown(path, owner.uid, owner.gid); err != nil {
			return "", err
		}
	}
	return key, nil
}

func readOrCreateKeyFile(path string, mode os.FileMode) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		key, err := newKey()
		if err != nil {
			return "", err
		}
		content := "MAGPIE_WEB_KEY=" + key + "\n"
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			return "", err
		}
		// WriteFile's mode is masked by the umask, and a reader on the other
		// side of the mount cannot tell a tight umask from a missing key:
		// set what this file is meant to be.
		if err := os.Chmod(path, mode); err != nil {
			return "", err
		}
		log.Printf("bridge: generated a new web key in %s", path)
		return key, nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "MAGPIE_WEB_KEY="); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), nil
		}
	}
	return "", errors.New("no MAGPIE_WEB_KEY line")
}

func newKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// fetchShim rewrites the page's root-absolute fetch URLs ("/api/…", "/v1/…")
// under the gateway prefix, so the page works from /app/magpie/ in the
// browser. Injected inline into the page's HTML by the bridge; the page's
// own assets are all relative already.
const fetchShim = `<script>
(function(){
"use strict";
var m=/^\/app\/[^\/]+\//.exec(location.pathname);
if(!m||typeof window.fetch!=="function")return;
var base=m[0].replace(/\/+$/,"");
function fix(u){
	if(typeof u==="string"&&u.charAt(0)==="/"&&u.charAt(1)!=="/")return base+u;
	return u;
}
var orig=window.fetch;
window.fetch=function(input,init){
	try{
		if(typeof input==="string")input=fix(input);
	}catch(e){}
	return orig.call(window,input,init);
};
})();
</script>`

// newBridge wires the config and the fixed upstream into the handler the
// Unix socket serves.
func newBridge(cfg config, upstream *url.URL) http.Handler {
	// A non-admin page view is normal (the fnOS entry is admin-only, but a
	// link can still be opened by anyone signed in), so say why the cookie
	// is missing once instead of on every asset the page asks for.
	var nonAdminOnce sync.Once
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(upstream)
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, cfg.prefix)
			if pr.Out.URL.Path == "" {
				pr.Out.URL.Path = "/"
			}
			pr.Out.URL.RawPath = ""
		},
		FlushInterval: -1, // stream model responses as they arrive
		ErrorHandler: func(rw http.ResponseWriter, r *http.Request, err error) {
			log.Printf("proxy %s %s: %v", r.Method, r.URL.Path, err)
			http.Error(rw, "magpie upstream does not answer: "+err.Error(), http.StatusBadGateway)
		},
		ModifyResponse: func(resp *http.Response) error {
			if !cfg.shimEnabled {
				return nil
			}
			// The page route only: magpie serves it gzipped when the
			// client accepts it, which would hide the HTML from the
			// injection. The bridge asks for that route uncompressed.
			p := strings.TrimPrefix(resp.Request.URL.Path, cfg.prefix)
			if (p != "/" && p != "/index.html") || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
				return nil
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			html := injectShim(string(body), fetchShim)
			resp.Body = io.NopCloser(strings.NewReader(html))
			resp.Header.Del("Content-Length")
			resp.Header.Del("Content-Encoding")
			resp.Header.Del("ETag")
			resp.Header.Set("Content-Length", fmt.Sprint(len(html)))
			return nil
		},
	}
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Absolute-form request targets and CONNECT would name a
		// destination of their own; this bridge has exactly one.
		if r.Method == http.MethodConnect || strings.HasPrefix(r.URL.Path, "//") || strings.Contains(r.URL.Path, "://") {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		// The fnOS desktop entry names /app/magpie without the trailing
		// slash; relative asset URLs need the slash, so send browsers there.
		if r.URL.Path == cfg.prefix {
			target := cfg.prefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(rw, r, target, http.StatusPermanentRedirect)
			return
		}
		if !strings.HasPrefix(r.URL.Path, cfg.prefix+"/") {
			http.NotFound(rw, r)
			return
		}
		// fnOS has authenticated this browser's session; hand it magpie
		// web's key cookie so the page opens without pasting the key. The
		// console changes every account, gateway key and route the device
		// has: the gateway only vouches that someone is signed in, so the
		// cookie goes to an administrator alone. Everyone else keeps
		// magpie's own sign-in form, which they cannot pass without the key.
		if cfg.sessionAdmin(r) {
			if !hasCookie(r, cfg.cookieName) {
				r.AddCookie(&http.Cookie{Name: cfg.cookieName, Value: cfg.cookieValue})
			}
		} else if cfg.sessionUser(r) {
			nonAdminOnce.Do(func() {
				log.Printf("bridge: user %s is not an fnOS administrator; magpie's web key is not injected for them", r.Header.Get(cfg.authHeader))
			})
		}
		// The page route goes uncompressed so ModifyResponse can inject.
		p := strings.TrimPrefix(r.URL.Path, cfg.prefix)
		if cfg.shimEnabled && (p == "/" || p == "/index.html") {
			r.Header.Set("Accept-Encoding", "identity")
		}
		proxy.ServeHTTP(rw, r)
	})
}

func main() {
	log.SetFlags(0)
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	upstream, err := url.Parse(cfg.upstream)
	if err != nil {
		log.Fatal(err)
	}
	if err := serve(cfg.socket, newBridge(cfg, upstream)); err != nil {
		log.Fatal(err)
	}
}

// injectShim puts the shim right after the page's <head> tag, before every
// script the page itself loads. A page without the tag is left alone.
func injectShim(html, shim string) string {
	i := strings.Index(strings.ToLower(html), "<head>")
	if i < 0 {
		return html
	}
	return html[:i+len("<head>")] + shim + html[i+len("<head>"):]
}

func hasCookie(r *http.Request, name string) bool {
	for _, c := range r.Cookies() {
		if c.Name == name {
			return true
		}
	}
	return false
}

// sessionUser reports whether the gateway forwarded a signed-in user. None
// but the gateway can reach the socket, so the header is the gateway's word.
func (c config) sessionUser(r *http.Request) bool {
	return c.authHeader != "" && r.Header.Get(c.authHeader) != ""
}

// sessionAdmin reports whether the gateway says that user is an
// administrator. It sends X-Trim-Isadmin for every session, true or false,
// so only a literal true counts - a missing header is not an administrator.
func (c config) sessionAdmin(r *http.Request) bool {
	if c.adminHeader == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get(c.adminHeader)), "true")
}

func serve(socket string, handler http.Handler) error {
	if filepath.Dir(socket) == "" {
		return errors.New("bridge: bad socket path")
	}
	// A leftover file or socket at the path would fail the listen; the
	// bridge owns this path (it lives inside the app's own directory).
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("bridge: remove stale socket: %w", err)
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("bridge: listen %s: %w", socket, err)
	}
	// The fnOS gateway process on the host connects from outside the
	// container; the socket lives in the app's own target directory, which
	// is already access-controlled by fnOS.
	if err := os.Chmod(socket, 0o666); err != nil {
		return fmt.Errorf("bridge: chmod socket: %w", err)
	}
	log.Printf("bridge: %s -> %s (prefix %s)", socket, os.Getenv("BRIDGE_UPSTREAM"), os.Getenv("BRIDGE_PREFIX"))
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 0} // streams and websockets stay open
	return srv.Serve(ln)
}
