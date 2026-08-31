package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
)

var cnpgClusterGVR = schema.GroupVersionResource{
	Group: "postgresql.cnpg.io", Version: "v1", Resource: "clusters",
}

type cnpgStatus struct {
	Namespace            string   `json:"namespace"`
	Name                 string   `json:"name"`
	Instances            int64    `json:"instances"`
	ReadyInstances       int64    `json:"readyInstances"`
	Primary              string   `json:"primary"`
	Phase                string   `json:"phase"`
	InstanceNames        []string `json:"instanceNames,omitempty"`
	FirstRecoverability  string   `json:"firstRecoverabilityPoint,omitempty"`
	LastSuccessfulBackup string   `json:"lastSuccessfulBackup,omitempty"`
	LastArchivedWAL      string   `json:"lastArchivedWAL,omitempty"`
}

func registerCNPG(s *server.MCPServer, kc *kube.Clients) {
	s.AddTool(
		mcp.NewTool("cnpg_status",
			mcp.WithDescription("Status of CloudNativePG clusters: topology, primary, readiness, backup and WAL archiving recency."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			list, err := kc.Dynamic.Resource(cnpgClusterGVR).Namespace("").List(ctx, metav1.ListOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			out := make([]cnpgStatus, 0, len(list.Items))
			for _, item := range list.Items {
				out = append(out, cnpgStatusOf(&item))
			}
			return jsonResult(out)
		},
	)
}

func cnpgStatusOf(u *unstructured.Unstructured) cnpgStatus {
	st := cnpgStatus{Namespace: u.GetNamespace(), Name: u.GetName()}
	st.Instances, _, _ = unstructured.NestedInt64(u.Object, "spec", "instances")
	st.ReadyInstances, _, _ = unstructured.NestedInt64(u.Object, "status", "readyInstances")
	st.Primary, _, _ = unstructured.NestedString(u.Object, "status", "currentPrimary")
	st.Phase, _, _ = unstructured.NestedString(u.Object, "status", "phase")
	st.InstanceNames, _, _ = unstructured.NestedStringSlice(u.Object, "status", "instanceNames")
	st.FirstRecoverability, _, _ = unstructured.NestedString(u.Object, "status", "firstRecoverabilityPoint")
	st.LastSuccessfulBackup, _, _ = unstructured.NestedString(u.Object, "status", "lastSuccessfulBackup")
	st.LastArchivedWAL, _, _ = unstructured.NestedString(u.Object, "status", "conditions", "lastArchivedWAL")
	if st.LastArchivedWAL == "" {
		st.LastArchivedWAL, _, _ = unstructured.NestedString(u.Object, "status", "lastArchivedWAL")
	}
	return st
}
