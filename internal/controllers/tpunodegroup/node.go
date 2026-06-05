package tpunodegroup

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/gce"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// labelTPUAccelerator is the label required by the device plugin DaemonSet.
	labelTPUAccelerator = "cloud.google.com/gke-tpu-accelerator"
	// labelTPUAcceleratorCount is the label identifying the number of TPU chips.
	labelTPUAcceleratorCount = "cloud.google.com/gke-accelerator-count"
	// labelTPUNodeGroup is the label identifying the TPUNodeGroup the node belongs to.
	labelTPUNodeGroup = "cloud.google.com/tpu-node-group"
	// labelTPUTopology is the label identifying the topology of the TPU slice.
	labelTPUTopology = "cloud.google.com/gke-tpu-topology"
)

// ReconcileNodes checks if nodes have joined the cluster, ensures they are labeled,
// and mutates NodeSummary in memory.
func ReconcileNodes(ctx context.Context, k8sClient client.Client, igmClient gce.IGMClient, recorder record.EventRecorder, group *tpuapi.TPUNodeGroup) error {
	// 1. Get list of expected instances from MIG
	// TODO: Get actual MIG name from status or child CR when available.
	migName := group.ManagedInstanceGroupName()
	instances, err := igmClient.ListManagedInstances(ctx, group.Spec.Project, group.Spec.NodeLocation, migName)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	// 2. List Node objects in the cluster
	var nodeList corev1.NodeList
	if err := k8sClient.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// 3. Build a map of nodes in the cluster for fast lookup by ProviderID
	nodeMap := make(map[string]*corev1.Node)
	for i := range nodeList.Items {
		if nodeList.Items[i].Spec.ProviderID != "" {
			nodeMap[nodeList.Items[i].Spec.ProviderID] = &nodeList.Items[i]
		}
	}

	readyCount := 0
	var errs []error

	// 4. Iterate over expected instances and match with nodes
	for _, inst := range instances {
		name := instanceShortName(inst.GetInstance())
		if name == "" {
			continue
		}

		providerID := fmt.Sprintf("gce://%s/%s/%s", group.Spec.Project, group.Spec.NodeLocation, name)

		if node, ok := nodeMap[providerID]; ok {
			// Check if node is ready
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyCount++
					// TODO: Cleanup bootstrap token secret for this node after it has joined.
					break
				}
			}

			// Ensure node has the required labels
			if err := ensureNodeLabels(ctx, k8sClient, node, group); err != nil {
				errs = append(errs, fmt.Errorf("failed to ensure labels for node %s: %w", node.Name, err))
			}
		}
	}

	if agg := utilerrors.NewAggregate(errs); agg != nil {
		return agg
	}

	var prevReady int32
	if group.Status.NodeSummary != nil {
		prevReady = group.Status.NodeSummary.Ready
	}

	// 4. Update TPUNodeGroup status
	if group.Status.NodeSummary == nil {
		group.Status.NodeSummary = &tpuapi.NodeSummary{}
	}
	currentReady := int32(readyCount)
	group.Status.NodeSummary.Ready = currentReady
	group.Status.NodeSummary.Reconciling = group.Spec.NodeCount - currentReady

	if currentReady > prevReady && currentReady < group.Spec.NodeCount {
		recorder.Event(group, corev1.EventTypeNormal, "NodesJoining", fmt.Sprintf("Waiting for %d of %d nodes to join the cluster", group.Spec.NodeCount-currentReady, group.Spec.NodeCount))
	}

	if currentReady < group.Spec.NodeCount {
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    tpuapi.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  tpuapi.ReasonAwaitingNodeJoin,
			Message: fmt.Sprintf("Waiting for %d of %d nodes to join the cluster", group.Spec.NodeCount-currentReady, group.Spec.NodeCount),
		})
	}

	return nil
}

// ensureNodeLabels adds required labels to the node if they are missing.
func ensureNodeLabels(ctx context.Context, k8sClient client.Client, node *corev1.Node, group *tpuapi.TPUNodeGroup) error {
	tpuNodeGroupLabelValue := fmt.Sprintf("%s-%s", group.Namespace, group.Name)

	acceleratorType := acceleratorLabelValue(group)
	if acceleratorType == "" {
		return nil // Skip if we cannot determine accelerator type
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	countStr := strconv.Itoa(chipsPerNode(group))
	needsUpdate := false
	if val, ok := node.Labels[labelTPUAccelerator]; !ok || val != acceleratorType {
		needsUpdate = true
	}
	if val, ok := node.Labels[labelTPUAcceleratorCount]; !ok || val != countStr {
		needsUpdate = true
	}
	if val, ok := node.Labels[labelTPUNodeGroup]; !ok || val != tpuNodeGroupLabelValue {
		needsUpdate = true
	}
	if group.Spec.Topology != "" {
		if val, ok := node.Labels[labelTPUTopology]; !ok || val != group.Spec.Topology {
			needsUpdate = true
		}
	} else {
		if _, ok := node.Labels[labelTPUTopology]; ok {
			needsUpdate = true
		}
	}

	if !needsUpdate {
		return nil
	}

	// Apply labels using Patch to avoid conflicts
	oldNode := node.DeepCopy()
	node.Labels[labelTPUAccelerator] = acceleratorType
	node.Labels[labelTPUAcceleratorCount] = countStr
	node.Labels[labelTPUNodeGroup] = tpuNodeGroupLabelValue
	if group.Spec.Topology != "" {
		node.Labels[labelTPUTopology] = group.Spec.Topology
	} else {
		delete(node.Labels, labelTPUTopology)
	}

	if err := k8sClient.Patch(ctx, node, client.MergeFrom(oldNode)); err != nil {
		return fmt.Errorf("failed to patch node labels: %w", err)
	}

	return nil
}

// instanceShortName extracts the instance name from its full URL.
func instanceShortName(url string) string {
	if url == "" {
		return ""
	}
	parts := strings.Split(url, "/")
	return parts[len(parts)-1]
}
