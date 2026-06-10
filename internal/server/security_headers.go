package server

import (
	"net/http"
	"net/url"
	"strings"
)

// contentSecurityPolicy is applied to every response. The admin/account UIs rely
// on inline <script>/<style> blocks and inline event-handler attributes, so
// script-src and style-src must permit 'unsafe-inline'. The policy still blocks
// loading scripts, styles, frames and objects from foreign origins, forbids the
// pages from being framed (clickjacking), and constrains form submissions and
// the document base URI. img-src allows https and data: so admin-configured
// poster/avatar URLs render; connect-src allows ws/wss for the live WebSocket.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' ws: wss:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// securityHeadersMiddleware sets defensive HTTP response headers on every
// response: a Content-Security-Policy, MIME-sniffing protection, clickjacking
// protection and a privacy-preserving Referrer-Policy.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// sameOriginWebSocket reports whether a WebSocket upgrade request may proceed.
// Requests without an Origin header (non-browser clients such as the launcher)
// are allowed. Browser-originated requests are only allowed when the Origin host
// matches the request Host, defeating Cross-Site WebSocket Hijacking. The
// connection is still unauthenticated until a valid access token is presented,
// so this is defence-in-depth.
func sameOriginWebSocket(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
