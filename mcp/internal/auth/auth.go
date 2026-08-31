package auth

import (
	"crypto/subtle"
	"net/http"
)

// HeaderName carries the API key on every request.
const HeaderName = "X-API-Key"

// APIKey rejects any request whose X-API-Key header does not match key,
// using a constant-time comparison.
func APIKey(key string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get(HeaderName)
		if subtle.ConstantTimeCompare([]byte(got), []byte(key)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
