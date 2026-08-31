package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cploutarchou/k3s-infra/mcp/internal/kube"
)

// capacityCeiling is the repo's HA rule: total requests stay under 2/3 of
// cluster capacity so two nodes can absorb the loss of one.
const capacityCeiling = 2.0 / 3.0

type haReport struct {
	NodesReady     int           `json:"nodesReady"`
	NodesTotal     int           `json:"nodesTotal"`
	EtcdHealthy    bool          `json:"etcdHealthy"`
	EtcdDetail     string        `json:"etcdDetail,omitempty"`
	ProblemPods    []string      `json:"problemPods"`
	CNPG           []cnpgStatus  `json:"cnpg"`
	Capacity       capacityUsage `json:"capacity"`
	Verdict        string        `json:"verdict"`
	VerdictReasons []string      `json:"verdictReasons,omitempty"`
}

type capacityUsage struct {
	CPURequested    string  `json:"cpuRequested"`
	CPUAllocatable  string  `json:"cpuAllocatable"`
	CPUFraction     float64 `json:"cpuFraction"`
	MemRequested    string  `json:"memoryRequested"`
	MemAllocatable  string  `json:"memoryAllocatable"`
	MemFraction     float64 `json:"memoryFraction"`
	WithinTwoThirds bool    `json:"withinTwoThirdsRule"`
}

func registerHA(s *server.MCPServer, kc *kube.Clients) {
	s.AddTool(
		mcp.NewTool("ha_report",
			mcp.WithDescription("One-shot HA posture report: node readiness, etcd health, non-running pods, CNPG topology and the 2/3 capacity rule."),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			rep := haReport{ProblemPods: []string{}, CNPG: []cnpgStatus{}}

			nodes, err := kc.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			rep.NodesTotal = len(nodes.Items)
			allocCPU, allocMem := resource.Quantity{}, resource.Quantity{}
			for _, n := range nodes.Items {
				for _, c := range n.Status.Conditions {
					if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
						rep.NodesReady++
					}
				}
				allocCPU.Add(*n.Status.Allocatable.Cpu())
				allocMem.Add(*n.Status.Allocatable.Memory())
			}

			body, err := kc.Clientset.Discovery().RESTClient().
				Get().AbsPath("/healthz/etcd").Do(ctx).Raw()
			if err != nil {
				rep.EtcdDetail = err.Error()
			} else {
				rep.EtcdHealthy = string(body) == "ok"
				rep.EtcdDetail = string(body)
			}

			pods, err := kc.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			reqCPU, reqMem := resource.Quantity{}, resource.Quantity{}
			for _, p := range pods.Items {
				if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodSucceeded {
					rep.ProblemPods = append(rep.ProblemPods,
						fmt.Sprintf("%s/%s (%s)", p.Namespace, p.Name, p.Status.Phase))
				}
				if p.Status.Phase == corev1.PodRunning || p.Status.Phase == corev1.PodPending {
					for _, c := range p.Spec.Containers {
						reqCPU.Add(*c.Resources.Requests.Cpu())
						reqMem.Add(*c.Resources.Requests.Memory())
					}
				}
			}

			cpuFrac := frac(reqCPU, allocCPU)
			memFrac := frac(reqMem, allocMem)
			rep.Capacity = capacityUsage{
				CPURequested:    reqCPU.String(),
				CPUAllocatable:  allocCPU.String(),
				CPUFraction:     cpuFrac,
				MemRequested:    reqMem.String(),
				MemAllocatable:  allocMem.String(),
				MemFraction:     memFrac,
				WithinTwoThirds: cpuFrac < capacityCeiling && memFrac < capacityCeiling,
			}

			cnpgList, err := kc.Dynamic.Resource(cnpgClusterGVR).Namespace("").List(ctx, metav1.ListOptions{})
			if err == nil {
				for _, item := range cnpgList.Items {
					rep.CNPG = append(rep.CNPG, cnpgStatusOf(&item))
				}
			}

			rep.Verdict = "healthy"
			if rep.NodesReady < rep.NodesTotal {
				rep.Verdict = "degraded"
				rep.VerdictReasons = append(rep.VerdictReasons, "not all nodes Ready")
			}
			if !rep.EtcdHealthy {
				rep.Verdict = "degraded"
				rep.VerdictReasons = append(rep.VerdictReasons, "etcd health check failed")
			}
			if len(rep.ProblemPods) > 0 {
				rep.Verdict = "degraded"
				rep.VerdictReasons = append(rep.VerdictReasons,
					fmt.Sprintf("%d pods not running", len(rep.ProblemPods)))
			}
			if !rep.Capacity.WithinTwoThirds {
				rep.Verdict = "degraded"
				rep.VerdictReasons = append(rep.VerdictReasons,
					"requests exceed 2/3 capacity rule — cannot absorb a node loss")
			}
			for _, c := range rep.CNPG {
				if c.ReadyInstances < c.Instances {
					rep.Verdict = "degraded"
					rep.VerdictReasons = append(rep.VerdictReasons,
						fmt.Sprintf("cnpg %s/%s: %d/%d instances ready", c.Namespace, c.Name, c.ReadyInstances, c.Instances))
				}
			}
			return jsonResult(rep)
		},
	)
}

func frac(req, alloc resource.Quantity) float64 {
	if alloc.IsZero() {
		return 0
	}
	return float64(req.MilliValue()) / float64(alloc.MilliValue())
}
