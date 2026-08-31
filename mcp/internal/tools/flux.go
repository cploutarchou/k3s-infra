package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
)

var fluxGVRs = map[string]schema.GroupVersionResource{
	"gitrepositories": {Group: "source.toolkit.fluxcd.io", Version: "v1", Resource: "gitrepositories"},
	"kustomizations":  {Group: "kustomize.toolkit.fluxcd.io", Version: "v1", Resource: "kustomizations"},
	"helmreleases":    {Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases"},
}

type fluxObjectStatus struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     string `json:"ready"`
	Suspended bool   `json:"suspended"`
	Message   string `json:"message"`
	Revision  string `json:"revision,omitempty"`
}

func registerFlux(s *server.MCPServer, kc *kube.Clients) {
	s.AddTool(
		mcp.NewTool("flux_status",
			mcp.WithDescription("Status of Flux GitRepositories, Kustomizations and HelmReleases: readiness, revision, last message."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out := []fluxObjectStatus{}
			for kind, gvr := range fluxGVRs {
				list, err := kc.Dynamic.Resource(gvr).Namespace("").List(ctx, metav1.ListOptions{})
				if err != nil {
					out = append(out, fluxObjectStatus{Kind: kind, Ready: "Unknown", Message: err.Error()})
					continue
				}
				for _, item := range list.Items {
					out = append(out, fluxStatusOf(kind, &item))
				}
			}
			return jsonResult(out)
		},
	)

	s.AddTool(
		mcp.NewTool("flux_reconcile",
			mcp.WithDescription("Ask Flux to reconcile one Kustomization or HelmRelease now (sets the reconcile.fluxcd.io/requestedAt annotation). This is the only sanctioned cluster write besides GitHub PRs."),
			mcp.WithString("kind", mcp.Required(), mcp.Description("kustomization or helmrelease")),
			mcp.WithString("namespace", mcp.Required(), mcp.Description("Object namespace (usually flux-system).")),
			mcp.WithString("name", mcp.Required(), mcp.Description("Object name.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			kind, err := req.RequireString("kind")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			ns, err := req.RequireString("namespace")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			name, err := req.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			var gvr schema.GroupVersionResource
			switch kind {
			case "kustomization":
				gvr = fluxGVRs["kustomizations"]
			case "helmrelease":
				gvr = fluxGVRs["helmreleases"]
			default:
				return mcp.NewToolResultError("kind must be kustomization or helmrelease"), nil
			}
			ts := time.Now().Format(time.RFC3339Nano)
			patch, _ := json.Marshal(map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						"reconcile.fluxcd.io/requestedAt": ts,
					},
				},
			})
			_, err = kc.Dynamic.Resource(gvr).Namespace(ns).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("reconcile of %s %s/%s requested at %s", kind, ns, name, ts)), nil
		},
	)
}

func fluxStatusOf(kind string, u *unstructured.Unstructured) fluxObjectStatus {
	st := fluxObjectStatus{
		Kind:      kind,
		Namespace: u.GetNamespace(),
		Name:      u.GetName(),
		Ready:     "Unknown",
	}
	if susp, found, _ := unstructured.NestedBool(u.Object, "spec", "suspend"); found {
		st.Suspended = susp
	}
	if rev, found, _ := unstructured.NestedString(u.Object, "status", "lastAppliedRevision"); found {
		st.Revision = rev
	} else if art, found, _ := unstructured.NestedString(u.Object, "status", "artifact", "revision"); found {
		st.Revision = art
	}
	conds, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["type"] == "Ready" {
			if v, ok := cm["status"].(string); ok {
				st.Ready = v
			}
			if v, ok := cm["message"].(string); ok {
				st.Message = v
			}
		}
	}
	return st
}
