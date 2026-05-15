package tpunodegroup

import (
	"context"
	"fmt"
	"strings"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// labelTPUAccelerator is the label required by the device plugin DaemonSet.
	labelTPUAccelerator = "cloud.google.com/gke-tpu-accelerator"
	// labelTPUNodeGroup is the label identifying the TPUNodeGroup the node belongs to.
	labelTPUNodeGroup   = "cloud.google.com/tpu-node-group"
	// labelTPUTopology is the label identifying the topology of the TPU slice.
	labelTPUTopology    = "cloud.google.com/gke-tpu-topology"
)

// ReconcileNodes checks if nodes have joined the cluster, ensures they are labeled,
// and mutates NodeSummary in memory.
func ReconcileNodes(ctx context.Context, k8sClient client.Client, igmClient gce.IGMClient, group *tpuapi.TPUNodeGroup) error {
	// 1. Get list of expected instances from MIG
	// TODO(b/500810349): Get actual MIG name from status or child CR when available.
	migName := group.Name
	instances, err := igmClient.ListManagedInstances(ctx, group.Spec.Project, group.Spec.NodeLocation, migName)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	// 2. List Node objects in the cluster
	var nodeList corev1.NodeList
	if err := k8sClient.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// 3. Build a map of nodes in the cluster for fast lookup
	nodeMap := make(map[string]*corev1.Node)
	for i := range nodeList.Items {
		nodeMap[nodeList.Items[i].Name] = &nodeList.Items[i]
	}

	readyCount := 0
	var errs []error

	// 4. Iterate over expected instances and match with nodes
	for _, inst := range instances {
		name := instanceShortName(inst.GetInstance())
		if name == "" {
			continue
		}

		if node, ok := nodeMap[name]; ok {
			// Check if node is ready
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyCount++
					// TODO(b/500810349): Cleanup bootstrap token secret for this node after it has joined.
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

	// 4. Update TPUNodeGroup status
	if group.Status.NodeSummary == nil {
		group.Status.NodeSummary = &tpuapi.NodeSummary{}
	}
	group.Status.NodeSummary.Ready = int32(readyCount)
	group.Status.NodeSummary.Reconciling = group.Spec.NodeCount - int32(readyCount)

	// TODO(b/500810349): Use providerID for lookup in the future.
	return nil
}

// ensureNodeLabels adds required labels to the node if they are missing.
func ensureNodeLabels(ctx context.Context, k8sClient client.Client, node *corev1.Node, group *tpuapi.TPUNodeGroup) error {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	tpuNodeGroupLabelValue := fmt.Sprintf("%s-%s", group.Namespace, group.Name)

	acceleratorType := acceleratorLabelValue(group)
	if acceleratorType == "" {
		return nil // Skip if we cannot determine accelerator type
	}
	needsUpdate := false
	if val, ok := node.Labels[labelTPUAccelerator]; !ok || val != acceleratorType {
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
