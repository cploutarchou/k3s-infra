// Package tools defines every MCP tool exposed by the server. Read-only
// tools inspect the cluster; the only write paths are GitHub PRs and
// triggering a Flux reconcile. Nothing here applies manifests, deletes
// resources, or mutates workloads — that is the repo contract.
package tools

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
)

// Register wires every tool onto the MCP server.
func Register(s *server.MCPServer, kc *kube.Clients) {
	registerReadOnly(s, kc)
	registerFlux(s, kc)
	registerCNPG(s, kc)
	registerHA(s, kc)
	registerGitHub(s)
}

// jsonResult marshals v as indented JSON into a tool text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
