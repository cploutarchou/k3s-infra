// k3s-infra MCP server: read-only cluster introspection plus a PR-based
// write path. Serves streamable HTTP at /mcp behind API-key header auth.
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/cploutarchou/k3s-infra/mcp/internal/auth"
	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
	"github.com/cploutarchou/k3s-infra/mcp/internal/tools"
)

const version = "0.2.1"

func main() {
	apiKey := os.Getenv("MCP_API_KEY")
	if apiKey == "" {
		log.Fatal("MCP_API_KEY must be set; refusing to serve unauthenticated")
	}

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

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/mcp", auth.APIKey(apiKey, mcpHandler))

	addr := os.Getenv("MCP_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("k3s-infra MCP server %s listening on %s", version, addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
