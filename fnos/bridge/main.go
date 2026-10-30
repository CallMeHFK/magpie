// Command bridge is the fnOS unified-gateway adapter for the magpie app
// package. The fnOS gateway authenticates a session, then forwards
// /app/magpie/* to a Unix socket this process listens on; the bridge strips
// the gateway prefix, hands fnOS-authenticated browsers magpie's web key as
// a cookie, and proxies everything to magpie web's one-port server (the
// page, its /api/* and the gateway client routes /v1, /v1beta, /mcp on the
// same port).
//
// The upstream address is fixed at startup from the environment (the app's
// compose file); it is never taken from a request, so there is no
// request-driven egress: every byte goes to that one upstream and nowhere
// else.
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	socket      string // BRIDGE_SOCKET, the Unix socket to listen on
	upstream    string // BRIDGE_UPSTREAM, the fixed http(s) URL to proxy to
	prefix      string // BRIDGE_PREFIX, the gateway path prefix to strip
	cookieName  string // BRIDGE_COOKIE_NAME, magpie web's key cookie
	cookieValue string // BRIDGE_COOKIE_VALUE, MAGPIE_WEB_KEY
	authHeader  string // BRIDGE_AUTH_HEADER, the fnOS gateway user header
	shimEnabled bool   // BRIDGE_FETCH_SHIM=1, rewrite absolute fetch URLs
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
		{"BRIDGE_AUTH_HEADER", &c.authHeader},
	} {
		v := os.Getenv(e.name)
		if v == "" {
			return c, fmt.Errorf("bridge: %s is required", e.name)
		}
		*e.dst = v
	}
	// The web key comes in as MAGPIE_WEB_KEY (the same env_file the magpie
	// container reads); BRIDGE_COOKIE_VALUE overrides it for standalone use.
	c.cookieValue = os.Getenv("BRIDGE_COOKIE_VALUE")
	if c.cookieValue == "" {
		c.cookieValue = os.Getenv("MAGPIE_WEB_KEY")
	}
	if c.cookieValue == "" {
		return c, fmt.Errorf("bridge: BRIDGE_COOKIE_VALUE or MAGPIE_WEB_KEY is required")
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
	if len(c.cookieValue) < 16 {
		return c, fmt.Errorf("bridge: BRIDGE_COOKIE_VALUE too short (min 16)")
	}
	c.shimEnabled = os.Getenv("BRIDGE_FETCH_SHIM") == "1"
	return c, nil
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
		// web's key cookie so the page opens without pasting the key.
		// Requests without the fnOS header (none can reach the socket but
		// through the gateway) keep magpie's own sign-in form.
		if cfg.authHeader != "" && r.Header.Get(cfg.authHeader) != "" {
			if !hasCookie(r, cfg.cookieName) {
				r.AddCookie(&http.Cookie{Name: cfg.cookieName, Value: cfg.cookieValue})
			}
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

func serve(socket string, handler http.Handler) error {
	if filepath.Dir(socket) == "" {
		return errors.New("bridge: bad socket path")
	}
	if fi, err := os.Stat(socket); err == nil && fi.Mode()&os.ModeSocket != 0 {
		if err := os.Remove(socket); err != nil {
			return fmt.Errorf("bridge: remove stale socket: %w", err)
		}
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
