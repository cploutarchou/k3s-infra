package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// HeaderName carries the API key on every request (alternative to
// Authorization: Bearer).
const HeaderName = "X-API-Key"

const maxBodyPeek = 1 << 20

// publicMethods is the JSON-RPC subset connector clients (e.g. claude.ai)
// probe before any credential is configured. It exposes only the server
// name and tool names/schemas — no cluster data. Everything else,
// tools/call above all, requires the key.
var publicMethods = map[string]bool{
	"initialize":                true,
	"notifications/initialized": true,
	"tools/list":                true,
	"ping":                      true,
}

// APIKey admits authenticated requests (X-API-Key header or Authorization:
// Bearer, both constant-time compared) and the unauthenticated handshake
// subset. Unauthorized calls get 401 with a WWW-Authenticate header.
func APIKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authorized(r, key) {
			next.ServeHTTP(w, r)
			return
		}
		// GET opens the server->client stream, DELETE ends a session;
		// neither carries tool input or returns tool output.
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyPeek))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var probe struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &probe) == nil && publicMethods[probe.Method] {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp", error="invalid_token"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func authorized(r *http.Request, key string) bool {
	if got := r.Header.Get(HeaderName); got != "" &&
		subtle.ConstantTimeCompare([]byte(got), []byte(key)) == 1 {
		return true
	}
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(ah, "Bearer ")), []byte(key)) == 1
	}
	return false
}
