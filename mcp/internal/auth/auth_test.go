package auth

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "test-key-0123456789abcdef"

// echo is the protected handler: it records that it ran and echoes the body
// so tests can prove the peeked body reaches it intact.
type echo struct{ calls int }

func (e *echo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.calls++
	body, _ := io.ReadAll(r.Body)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func newGuard(t *testing.T, public bool) (*Middleware, *echo, *bytes.Buffer) {
	t.Helper()
	next := &echo{}
	logs := &bytes.Buffer{}
	m, err := New(Config{Key: testKey, PublicHandshake: public, Logger: log.New(logs, "", 0)}, next)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m, next, logs
}

func do(m *Middleware, method, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	m.ServeHTTP(rec, req)
	return rec
}

const (
	callBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"logs","arguments":{"namespace":"kube-system","pod":"x"}}}`
	initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
)

func TestToolsCallRequiresValidCredential(t *testing.T) {
	cases := []struct {
		name      string
		headers   map[string]string
		wantCode  int
		wantChall string // exact WWW-Authenticate value on 401
	}{
		{"no header", nil, 401, `Bearer realm="mcp"`},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"bare scheme", map[string]string{"Authorization": "Bearer"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"unexpanded shell variable", map[string]string{"Authorization": "Bearer ${K3S_MCP_TOKEN}"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"key with one extra char", map[string]string{"Authorization": "Bearer " + testKey + "x"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"key truncated", map[string]string{"Authorization": "Bearer " + testKey[:len(testKey)-1]}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"basic scheme", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"empty api key header", map[string]string{HeaderName: ""}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"wrong api key header", map[string]string{HeaderName: "nope"}, 401, `Bearer realm="mcp", error="invalid_token"`},
		{"correct bearer", map[string]string{"Authorization": "Bearer " + testKey}, 200, ""},
		{"correct bearer, lowercase scheme", map[string]string{"Authorization": "bearer " + testKey}, 200, ""},
		{"correct api key header", map[string]string{HeaderName: testKey}, 200, ""},
		{"correct api key header, odd case", map[string]string{"x-api-key": testKey}, 200, ""},
		{"valid api key beside a stale bearer", map[string]string{HeaderName: testKey, "Authorization": "Bearer stale"}, 200, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, next, logs := newGuard(t, true)
			rec := do(m, http.MethodPost, callBody, tc.headers)
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode == 200 {
				if next.calls != 1 {
					t.Fatalf("protected handler calls = %d, want 1", next.calls)
				}
				if rec.Body.String() != callBody {
					t.Fatalf("body did not reach the handler intact: %q", rec.Body.String())
				}
				return
			}
			if next.calls != 0 {
				t.Fatalf("protected handler ran %d times on a rejected request", next.calls)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantChall {
				t.Fatalf("WWW-Authenticate = %q, want %q", got, tc.wantChall)
			}
			if !strings.Contains(logs.String(), "auth: rejected POST /mcp") {
				t.Fatalf("rejection not logged: %q", logs.String())
			}
			if strings.Contains(logs.String(), testKey) || strings.Contains(logs.String(), "nope") {
				t.Fatalf("log line leaks a credential value: %q", logs.String())
			}
		})
	}
}

func TestPublicHandshakeIsAnonymousOnlyWithoutCredential(t *testing.T) {
	for _, method := range []string{"initialize", "notifications/initialized", "tools/list", "ping"} {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"}`
		t.Run(method+" anonymous", func(t *testing.T) {
			m, next, _ := newGuard(t, true)
			rec := do(m, http.MethodPost, body, nil)
			if rec.Code != 200 || next.calls != 1 {
				t.Fatalf("status = %d, calls = %d; want 200 and 1", rec.Code, next.calls)
			}
			if rec.Body.String() != body {
				t.Fatalf("peeked body not restored: %q", rec.Body.String())
			}
		})
		t.Run(method+" with a wrong credential", func(t *testing.T) {
			m, next, _ := newGuard(t, true)
			rec := do(m, http.MethodPost, body, map[string]string{"Authorization": "Bearer wrong"})
			if rec.Code != 401 || next.calls != 0 {
				t.Fatalf("status = %d, calls = %d; want 401 and 0 — a presented credential must be valid even on the handshake", rec.Code, next.calls)
			}
		})
		t.Run(method+" with the right credential", func(t *testing.T) {
			m, next, _ := newGuard(t, true)
			rec := do(m, http.MethodPost, body, map[string]string{HeaderName: testKey})
			if rec.Code != 200 || next.calls != 1 {
				t.Fatalf("status = %d, calls = %d; want 200 and 1", rec.Code, next.calls)
			}
		})
	}
}

func TestAnonymousNonHandshakeMethodsAreRejected(t *testing.T) {
	for _, method := range []string{"tools/call", "resources/list", "resources/read", "prompts/list", "completion/complete", "logging/setLevel", ""} {
		body := `{"jsonrpc":"2.0","id":1,"method":"` + method + `"}`
		t.Run("method "+method, func(t *testing.T) {
			m, next, _ := newGuard(t, true)
			rec := do(m, http.MethodPost, body, nil)
			if rec.Code != 401 || next.calls != 0 {
				t.Fatalf("status = %d, calls = %d; want 401 and 0", rec.Code, next.calls)
			}
		})
	}
}

func TestAnonymousUnparseableBodiesAreRejected(t *testing.T) {
	cases := map[string]string{
		"batch of public methods": `[{"jsonrpc":"2.0","id":1,"method":"initialize"}]`,
		"batch hiding a call":     `[{"jsonrpc":"2.0","id":1,"method":"initialize"},{"jsonrpc":"2.0","id":2,"method":"tools/call"}]`,
		"not json":                `initialize`,
		"empty":                   ``,
		"method not a string":     `{"method":123}`,
		"method null":             `{"method":null}`,
		"duplicate method keys":   `{"method":"initialize","method":"tools/call"}`,
		"oversized":               `{"method":"initialize","pad":"` + strings.Repeat("a", maxBodyPeek) + `"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			m, next, _ := newGuard(t, true)
			rec := do(m, http.MethodPost, body, nil)
			if rec.Code != 401 || next.calls != 0 {
				t.Fatalf("status = %d, calls = %d; want 401 and 0", rec.Code, next.calls)
			}
		})
	}
}

func TestAuthenticatedBodiesAreNeverPeekedOrTruncated(t *testing.T) {
	m, next, _ := newGuard(t, true)
	big := `{"method":"tools/call","pad":"` + strings.Repeat("b", 2*maxBodyPeek) + `"}`
	rec := do(m, http.MethodPost, big, map[string]string{HeaderName: testKey})
	if rec.Code != 200 || next.calls != 1 {
		t.Fatalf("status = %d, calls = %d; want 200 and 1", rec.Code, next.calls)
	}
	if rec.Body.Len() != len(big) {
		t.Fatalf("authenticated body truncated: got %d bytes, want %d", rec.Body.Len(), len(big))
	}
}

func TestTransportVerbs(t *testing.T) {
	t.Run("anonymous GET passes with public handshake", func(t *testing.T) {
		m, next, _ := newGuard(t, true)
		if rec := do(m, http.MethodGet, "", nil); rec.Code != 200 || next.calls != 1 {
			t.Fatalf("status = %d, calls = %d; want 200 and 1", rec.Code, next.calls)
		}
	})
	t.Run("GET with a wrong credential is rejected", func(t *testing.T) {
		m, next, _ := newGuard(t, true)
		if rec := do(m, http.MethodGet, "", map[string]string{HeaderName: "nope"}); rec.Code != 401 || next.calls != 0 {
			t.Fatalf("status = %d, calls = %d; want 401 and 0", rec.Code, next.calls)
		}
	})
	t.Run("anonymous DELETE passes with public handshake", func(t *testing.T) {
		m, next, _ := newGuard(t, true)
		if rec := do(m, http.MethodDelete, "", nil); rec.Code != 200 || next.calls != 1 {
			t.Fatalf("status = %d, calls = %d; want 200 and 1", rec.Code, next.calls)
		}
	})
}

func TestStrictModeRequiresTheKeyEverywhere(t *testing.T) {
	m, next, _ := newGuard(t, false)
	for name, req := range map[string]func() *httptest.ResponseRecorder{
		"initialize": func() *httptest.ResponseRecorder { return do(m, http.MethodPost, initBody, nil) },
		"tools/list": func() *httptest.ResponseRecorder { return do(m, http.MethodPost, `{"method":"tools/list"}`, nil) },
		"ping":       func() *httptest.ResponseRecorder { return do(m, http.MethodPost, `{"method":"ping"}`, nil) },
		"tools/call": func() *httptest.ResponseRecorder { return do(m, http.MethodPost, callBody, nil) },
		"GET stream": func() *httptest.ResponseRecorder { return do(m, http.MethodGet, "", nil) },
		"DELETE":     func() *httptest.ResponseRecorder { return do(m, http.MethodDelete, "", nil) },
		"wrong key": func() *httptest.ResponseRecorder {
			return do(m, http.MethodPost, initBody, map[string]string{HeaderName: "nope"})
		},
		"empty token": func() *httptest.ResponseRecorder {
			return do(m, http.MethodPost, initBody, map[string]string{"Authorization": "Bearer "})
		},
	} {
		if rec := req(); rec.Code != 401 || rec.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("%s: status = %d, challenge %q; want 401 with a challenge", name, rec.Code, rec.Header().Get("WWW-Authenticate"))
		}
	}
	if next.calls != 0 {
		t.Fatalf("protected handler ran %d times", next.calls)
	}
	for _, h := range []map[string]string{{HeaderName: testKey}, {"Authorization": "Bearer " + testKey}} {
		if rec := do(m, http.MethodPost, initBody, h); rec.Code != 200 {
			t.Fatalf("valid credential rejected in strict mode: %d", rec.Code)
		}
	}
	if next.calls != 2 {
		t.Fatalf("protected handler calls = %d, want 2", next.calls)
	}
}

func TestNewRefusesEmptyKey(t *testing.T) {
	if _, err := New(Config{Key: ""}, &echo{}); err == nil {
		t.Fatal("New accepted an empty key")
	}
	if _, err := New(Config{Key: testKey}, nil); err == nil {
		t.Fatal("New accepted a nil handler")
	}
}

func TestClientIPPrefersProxyHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.RemoteAddr = "10.42.0.7:51234"
	if got := clientIP(req); got != "10.42.0.7" {
		t.Fatalf("peer only: %q", got)
	}
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.42.0.1")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("xff: %q", got)
	}
	req.Header.Set("Cf-Connecting-Ip", "198.51.100.4")
	if got := clientIP(req); got != "198.51.100.4" {
		t.Fatalf("cloudflare: %q", got)
	}
}
