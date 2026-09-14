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

// readOnly annotates an observational tool: it never mutates the cluster,
// repeating it changes nothing, and it talks only to the cluster API.
// mcp-go's defaults (readOnlyHint false, destructiveHint true) would make a
// client treat "list nodes" like a destructive action.
func readOnly(title string) mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		Title:           title,
		ReadOnlyHint:    mcp.ToBoolPtr(true),
		DestructiveHint: mcp.ToBoolPtr(false),
		IdempotentHint:  mcp.ToBoolPtr(true),
		OpenWorldHint:   mcp.ToBoolPtr(false),
	})
}

// additiveWrite annotates a write that only adds (a PR, a reconcile
// request) and never destroys anything. idempotent says whether repeating
// the same call changes anything further; openWorld whether it leaves the
// cluster (GitHub).
func additiveWrite(title string, idempotent, openWorld bool) mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		Title:           title,
		ReadOnlyHint:    mcp.ToBoolPtr(false),
		DestructiveHint: mcp.ToBoolPtr(false),
		IdempotentHint:  mcp.ToBoolPtr(idempotent),
		OpenWorldHint:   mcp.ToBoolPtr(openWorld),
	})
}

// jsonResult marshals v as indented JSON into a tool text result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}
