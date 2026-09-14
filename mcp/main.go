// k3s-infra MCP server: read-only cluster introspection plus a PR-based
// write path. Serves streamable HTTP at /mcp behind API-key auth.
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/mark3labs/mcp-go/server"

	"github.com/cploutarchou/k3s-infra/mcp/internal/auth"
	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
	"github.com/cploutarchou/k3s-infra/mcp/internal/tools"
)

const version = "0.3.0"

func main() {
	apiKey := os.Getenv("MCP_API_KEY")
	if apiKey == "" {
		log.Fatal("MCP_API_KEY must be set; refusing to serve unauthenticated")
	}
	publicHandshake := envBool("MCP_PUBLIC_HANDSHAKE", true)

	kc, err := kube.NewClients()
	if err != nil {
		log.Fatalf("kubernetes client init: %v", err)
	}

	s := server.NewMCPServer("k3s-infra", version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
	)
	tools.Register(s, kc)

	// Stateless: no in-memory session affinity, so the deployment can run
	// multiple replicas behind one Service without sticky routing.
	mcpHandler := server.NewStreamableHTTPServer(s,
		server.WithEndpointPath("/mcp"),
		server.WithStateLess(true),
	)

	guard, err := auth.New(auth.Config{Key: apiKey, PublicHandshake: publicHandshake}, mcpHandler)
	if err != nil {
		log.Fatalf("auth: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/mcp", guard)

	addr := os.Getenv("MCP_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("k3s-infra MCP server %s listening on %s (public handshake: %v)", version, addr, publicHandshake)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// envBool reads a boolean environment variable; unset means def, anything
// unparseable is fatal so a typo cannot silently pick a mode.
func envBool(name string, def bool) bool {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		log.Fatalf("%s=%q: want true or false", name, raw)
	}
	return v
}
