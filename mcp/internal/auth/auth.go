// Package auth guards the MCP endpoint with a single shared API key.
//
// Policy:
//
//   - A request that presents a credential (X-API-Key and/or Authorization)
//     must present a valid one, whatever the JSON-RPC method or HTTP verb.
//     A wrong, empty or malformed credential is always 401 — it is never
//     admitted as if it were an anonymous handshake probe.
//   - A request with no credential is admitted only for the MCP handshake
//     subset (initialize, notifications/initialized, tools/list, ping) and
//     the GET/DELETE transport verbs, and only while Config.PublicHandshake
//     is on. The handshake exposes the server name/version and the tool
//     schemas — never cluster data. Everything else, tools/call above all,
//     is 401 with a WWW-Authenticate challenge.
//   - Comparison is constant-time over SHA-256 digests, so neither the key
//     nor its length leaks through timing.
//   - Credential values are never logged. A rejection logs only whether a
//     credential was present.
package auth

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
)

// HeaderName carries the API key as an alternative to
// "Authorization: Bearer <key>". claude.ai custom connectors send this
// header on every request.
const HeaderName = "X-API-Key"

// maxBodyPeek bounds how much of an anonymous POST body is read to find the
// JSON-RPC method. Larger bodies are treated as unparseable (rejected).
const maxBodyPeek = 1 << 20

var headerKey = http.CanonicalHeaderKey(HeaderName)

// publicMethods is the handshake subset a connector client calls before its
// credential is configured. Admitted anonymously only with PublicHandshake.
var publicMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"tools/list":                true,
	"ping":                      true,
}

// Config configures the middleware.
type Config struct {
	// Key is the shared secret. Empty is refused at construction.
	Key string
	// PublicHandshake admits the handshake subset and the GET/DELETE
	// transport verbs without a credential. Off means every request needs
	// the key, including the connector's pre-credential probe.
	PublicHandshake bool
	// Logger receives one line per rejected request (presence only, never
	// values). nil means log.Default().
	Logger *log.Logger
}

// Middleware is the http.Handler that enforces Config in front of the MCP
// transport handler.
type Middleware struct {
	keyHash         [sha256.Size]byte
	publicHandshake bool
	logger          *log.Logger
	next            http.Handler
}

// New builds the middleware. It refuses an empty key so a misconfigured
// deployment fails at start instead of serving unauthenticated.
func New(cfg Config, next http.Handler) (*Middleware, error) {
	if cfg.Key == "" {
		return nil, errors.New("auth: empty API key; refusing to serve unauthenticated")
	}
	if next == nil {
		return nil, errors.New("auth: nil next handler")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &Middleware{
		keyHash:         sha256.Sum256([]byte(cfg.Key)),
		publicHandshake: cfg.PublicHandshake,
		logger:          logger,
		next:            next,
	}, nil
}

// ServeHTTP implements http.Handler.
func (m *Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	presented, valid := m.credentials(r)
	switch {
	case valid:
		m.next.ServeHTTP(w, r)
	case presented:
		m.reject(w, r, "invalid", "")
	case !m.publicHandshake:
		m.reject(w, r, "none", "")
	case r.Method != http.MethodPost:
		// GET opens the server->client notification stream and DELETE ends
		// a session. In stateless mode neither carries tool input or output.
		m.next.ServeHTTP(w, r)
	default:
		method, err := peekMethod(r)
		if err != nil {
			// The body could not be read: a transport failure, not an
			// authorization decision.
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if publicMethods[method] {
			m.next.ServeHTTP(w, r)
			return
		}
		m.reject(w, r, "none", method)
	}
}

// credentials reports whether the request carries any credential header and
// whether at least one carried value matches the key. Every value is checked
// (no early exit) and each comparison is constant-time.
func (m *Middleware) credentials(r *http.Request) (presented, valid bool) {
	if vals, ok := r.Header[headerKey]; ok {
		presented = true
		for _, v := range vals {
			valid = m.match(v) || valid
		}
	}
	if vals, ok := r.Header["Authorization"]; ok {
		presented = true
		for _, v := range vals {
			tok, isBearer := bearerToken(v)
			valid = (isBearer && m.match(tok)) || valid
		}
	}
	return presented, valid
}

// match compares a candidate against the key in constant time, via SHA-256
// so the comparison does not short-circuit on length.
func (m *Middleware) match(candidate string) bool {
	sum := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(sum[:], m.keyHash[:]) == 1
}

// bearerToken extracts the token from an Authorization header value. The
// scheme is case-insensitive (RFC 7235); a missing or empty token yields an
// empty string, which can never match.
func bearerToken(v string) (token string, isBearer bool) {
	scheme, rest, found := strings.Cut(strings.TrimSpace(v), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// peekMethod reads the JSON-RPC method from a POST body and restores the
// body for the next handler. Unparseable, batch (array) and oversized bodies
// yield "" — which is never a public method. Only an I/O failure is an error.
func peekMethod(r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyPeek+1))
	if err != nil {
		return "", err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > maxBodyPeek {
		return "", nil
	}
	var probe struct {
		Method string `json:"method"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return "", nil
	}
	return probe.Method, nil
}

// reject answers 401 with an RFC 6750 challenge. credential is "none" or
// "invalid"; rpcMethod is the peeked JSON-RPC method when known.
func (m *Middleware) reject(w http.ResponseWriter, r *http.Request, credential, rpcMethod string) {
	challenge := `Bearer realm="mcp"`
	if credential == "invalid" {
		challenge += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	m.logger.Printf("auth: rejected %s %s credential=%s rpc=%q client=%s ua=%q",
		r.Method, r.URL.Path, credential, rpcMethod, clientIP(r), truncate(r.UserAgent(), 64))
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// clientIP prefers the Cloudflare and proxy headers over the TCP peer, which
// behind Traefik is always the proxy.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
