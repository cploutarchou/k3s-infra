package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// TestToolAnnotations pins the read/write contract the server advertises:
// every observational tool is read-only and non-destructive; the two writes
// are additive, never destructive. Register does not touch the cluster, so
// a nil client is fine here.
func TestToolAnnotations(t *testing.T) {
	s := server.NewMCPServer("test", "0.0.0", server.WithToolCapabilities(false))
	Register(s, nil)

	ctx := context.Background()
	s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`))
	raw, err := json.Marshal(s.HandleMessage(ctx, json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)))
	if err != nil {
		t.Fatalf("marshal tools/list response: %v", err)
	}
	var resp struct {
		Result mcp.ListToolsResult `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode tools/list response: %v (%s)", err, raw)
	}
	if resp.Error != nil {
		t.Fatalf("tools/list failed: %s", resp.Error.Message)
	}

	wantReadOnly := map[string]bool{
		"nodes":          true,
		"pods":           true,
		"events":         true,
		"logs":           true,
		"flux_status":    true,
		"cnpg_status":    true,
		"ha_report":      true,
		"flux_reconcile": false,
		"propose_change": false,
	}
	seen := map[string]bool{}
	for _, tool := range resp.Result.Tools {
		seen[tool.Name] = true
		ro, ok := wantReadOnly[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q — add it to the contract table", tool.Name)
			continue
		}
		a := tool.Annotations
		if a.ReadOnlyHint == nil || a.DestructiveHint == nil || a.IdempotentHint == nil || a.OpenWorldHint == nil {
			t.Errorf("%s: every hint must be set explicitly, got %+v", tool.Name, a)
			continue
		}
		if *a.ReadOnlyHint != ro {
			t.Errorf("%s: readOnlyHint = %v, want %v", tool.Name, *a.ReadOnlyHint, ro)
		}
		if *a.DestructiveHint {
			t.Errorf("%s: destructiveHint = true; nothing this server exposes is destructive", tool.Name)
		}
		if ro && (!*a.IdempotentHint || *a.OpenWorldHint) {
			t.Errorf("%s: a read-only tool must be idempotent and closed-world, got idempotent=%v openWorld=%v", tool.Name, *a.IdempotentHint, *a.OpenWorldHint)
		}
		if a.Title == "" {
			t.Errorf("%s: missing title annotation", tool.Name)
		}
	}
	for name := range wantReadOnly {
		if !seen[name] {
			t.Errorf("tool %q not registered", name)
		}
	}
	if pc := find(resp.Result.Tools, "propose_change"); pc != nil && !*pc.Annotations.OpenWorldHint {
		t.Errorf("propose_change talks to GitHub; openWorldHint must be true")
	}
}

func find(tools []mcp.Tool, name string) *mcp.Tool {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}
