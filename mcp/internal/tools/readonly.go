package tools

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
)

const maxLogBytes = 256 * 1024

func registerReadOnly(s *server.MCPServer, kc *kube.Clients) {
	s.AddTool(
		mcp.NewTool("nodes",
			mcp.WithDescription("List cluster nodes with roles, readiness, versions, IPs and allocatable resources."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			list, err := kc.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			type nodeInfo struct {
				Name        string            `json:"name"`
				Ready       bool              `json:"ready"`
				Roles       []string          `json:"roles"`
				InternalIP  string            `json:"internalIP"`
				Version     string            `json:"kubeletVersion"`
				OS          string            `json:"osImage"`
				Allocatable map[string]string `json:"allocatable"`
				Conditions  []string          `json:"abnormalConditions,omitempty"`
			}
			out := make([]nodeInfo, 0, len(list.Items))
			for _, n := range list.Items {
				ni := nodeInfo{
					Name:    n.Name,
					Version: n.Status.NodeInfo.KubeletVersion,
					OS:      n.Status.NodeInfo.OSImage,
					Allocatable: map[string]string{
						"cpu":    n.Status.Allocatable.Cpu().String(),
						"memory": n.Status.Allocatable.Memory().String(),
					},
				}
				for label := range n.Labels {
					if role, ok := roleFromLabel(label); ok {
						ni.Roles = append(ni.Roles, role)
					}
				}
				sort.Strings(ni.Roles)
				for _, c := range n.Status.Conditions {
					if c.Type == corev1.NodeReady {
						ni.Ready = c.Status == corev1.ConditionTrue
					} else if c.Status == corev1.ConditionTrue {
						ni.Conditions = append(ni.Conditions, string(c.Type))
					}
				}
				for _, a := range n.Status.Addresses {
					if a.Type == corev1.NodeInternalIP {
						ni.InternalIP = a.Address
					}
				}
				out = append(out, ni)
			}
			return jsonResult(out)
		},
	)

	s.AddTool(
		mcp.NewTool("pods",
			mcp.WithDescription("List pods. Defaults to all namespaces; set problemsOnly to filter to pods that are not Running/Succeeded."),
			mcp.WithString("namespace", mcp.Description("Namespace to list; empty for all.")),
			mcp.WithBoolean("problemsOnly", mcp.Description("Only pods not in Running or Succeeded phase, or not ready.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ns := req.GetString("namespace", "")
			problemsOnly := req.GetBool("problemsOnly", false)
			list, err := kc.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			type podInfo struct {
				Namespace string `json:"namespace"`
				Name      string `json:"name"`
				Phase     string `json:"phase"`
				Ready     string `json:"ready"`
				Restarts  int32  `json:"restarts"`
				Node      string `json:"node"`
				Reason    string `json:"reason,omitempty"`
			}
			out := []podInfo{}
			for _, p := range list.Items {
				ready, total, restarts := 0, len(p.Spec.Containers), int32(0)
				for _, cs := range p.Status.ContainerStatuses {
					if cs.Ready {
						ready++
					}
					restarts += cs.RestartCount
				}
				healthy := p.Status.Phase == corev1.PodRunning && ready == total ||
					p.Status.Phase == corev1.PodSucceeded
				if problemsOnly && healthy {
					continue
				}
				out = append(out, podInfo{
					Namespace: p.Namespace,
					Name:      p.Name,
					Phase:     string(p.Status.Phase),
					Ready:     fmt.Sprintf("%d/%d", ready, total),
					Restarts:  restarts,
					Node:      p.Spec.NodeName,
					Reason:    p.Status.Reason,
				})
			}
			return jsonResult(out)
		},
	)

	s.AddTool(
		mcp.NewTool("events",
			mcp.WithDescription("Recent cluster events, newest first."),
			mcp.WithString("namespace", mcp.Description("Namespace; empty for all.")),
			mcp.WithBoolean("warningsOnly", mcp.Description("Only Warning events.")),
			mcp.WithNumber("limit", mcp.Description("Maximum events to return (default 50).")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ns := req.GetString("namespace", "")
			limit := int(req.GetFloat("limit", 50))
			opts := metav1.ListOptions{}
			if req.GetBool("warningsOnly", false) {
				opts.FieldSelector = "type=Warning"
			}
			list, err := kc.Clientset.CoreV1().Events(ns).List(ctx, opts)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			sort.Slice(list.Items, func(i, j int) bool {
				return eventTime(&list.Items[i]).After(eventTime(&list.Items[j]))
			})
			if len(list.Items) > limit {
				list.Items = list.Items[:limit]
			}
			type eventInfo struct {
				Time      string `json:"time"`
				Type      string `json:"type"`
				Reason    string `json:"reason"`
				Object    string `json:"object"`
				Namespace string `json:"namespace"`
				Message   string `json:"message"`
				Count     int32  `json:"count"`
			}
			out := make([]eventInfo, 0, len(list.Items))
			for _, e := range list.Items {
				out = append(out, eventInfo{
					Time:      eventTime(&e).Format(time.RFC3339),
					Type:      e.Type,
					Reason:    e.Reason,
					Object:    fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
					Namespace: e.Namespace,
					Message:   e.Message,
					Count:     e.Count,
				})
			}
			return jsonResult(out)
		},
	)

	s.AddTool(
		mcp.NewTool("logs",
			mcp.WithDescription("Fetch logs from one pod container (capped at 256KiB)."),
			mcp.WithString("namespace", mcp.Required(), mcp.Description("Pod namespace.")),
			mcp.WithString("pod", mcp.Required(), mcp.Description("Pod name.")),
			mcp.WithString("container", mcp.Description("Container name; defaults to the first container.")),
			mcp.WithNumber("tailLines", mcp.Description("Number of lines from the end (default 200).")),
			mcp.WithBoolean("previous", mcp.Description("Logs from the previous (crashed) instance.")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			ns, err := req.RequireString("namespace")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			pod, err := req.RequireString("pod")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			tail := int64(req.GetFloat("tailLines", 200))
			opts := &corev1.PodLogOptions{
				Container: req.GetString("container", ""),
				TailLines: &tail,
				Previous:  req.GetBool("previous", false),
			}
			stream, err := kc.Clientset.CoreV1().Pods(ns).GetLogs(pod, opts).Stream(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			defer stream.Close()
			data, err := io.ReadAll(io.LimitReader(stream, maxLogBytes))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(data)), nil
		},
	)
}

func roleFromLabel(label string) (string, bool) {
	const prefix = "node-role.kubernetes.io/"
	if len(label) > len(prefix) && label[:len(prefix)] == prefix {
		return label[len(prefix):], true
	}
	return "", false
}

func eventTime(e *corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}
