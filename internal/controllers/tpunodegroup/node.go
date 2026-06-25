package tpunodegroup

import (
	"context"
	"fmt"
	"strconv"

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

	// resourceNameTPU is the canonical name for TPU resources exposed by the device plugin.
	resourceNameTPU corev1.ResourceName = "google.com/tpu"
)

// ReconcileNodes checks if nodes have joined the cluster, ensures they are labeled,
// and mutates NodeSummary in memory.
func ReconcileNodes(ctx context.Context, k8sClient client.Client, igmClient gce.IGMClient, recorder record.EventRecorder, group *tpuapi.TPUNodeGroup, machineType string) error {
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

	var machineType string
	if group.Spec.InstanceConfig != nil {
		machineType = group.Spec.InstanceConfig.MachineType
	}

	readyCount := 0
	var errs []error

	// 4. Iterate over expected instances and match with nodes
	for _, inst := range instances {
		name := extractShortName(inst.GetInstance())
		if name == "" {
			continue
		}

		providerID := fmt.Sprintf("gce://%s/%s/%s", group.Spec.Project, group.Spec.NodeLocation, name)

		if node, ok := nodeMap[providerID]; ok {
			// Check if node is ready and TPU resources match the expected GKE accelerator count
			if isNodeTPUReady(node, chipsPerNode(machineType)) {
				readyCount++
				// TODO: Cleanup bootstrap token secret for this node after it has joined.
			}

			// Ensure node has the required labels
			if err := ensureNodeLabels(ctx, k8sClient, node, group, machineType); err != nil {
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
func ensureNodeLabels(ctx context.Context, k8sClient client.Client, node *corev1.Node, group *tpuapi.TPUNodeGroup, machineType string) error {
	tpuNodeGroupLabelValue := fmt.Sprintf("%s-%s", group.Namespace, group.Name)

	acceleratorType := acceleratorLabelValue(machineType, group.Spec.Topology, string(group.Spec.TargetSizePolicyMode))
	if acceleratorType == "" {
		return nil // Skip if we cannot determine accelerator type
	}

	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	countStr := strconv.Itoa(chipsPerNode(machineType))
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

// isNodeTPUReady checks if the node is K8s Ready and has its TPU resources fully exposed, allocatable, and matching the expected chips count.
func isNodeTPUReady(node *corev1.Node, expectedChips int) bool {
	// 1. Check standard K8s NodeReady condition
	isK8sReady := false
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
			isK8sReady = true
			break
		}
	}
	if !isK8sReady {
		return false
	}

	// 2. Extract TPU resource Capacity and Allocatable
	capacity, hasCapacity := node.Status.Capacity[resourceNameTPU]
	allocatable, hasAllocatable := node.Status.Allocatable[resourceNameTPU]

	if !hasCapacity || !hasAllocatable {
		return false
	}

	// 3. Values must be non-zero
	if capacity.Value() <= 0 || allocatable.Value() <= 0 {
		return false
	}

	// 4. Capacity and Allocatable must match the expected chips count if specified.
	if expectedChips > 0 {
		if capacity.Value() != int64(expectedChips) || allocatable.Value() != int64(expectedChips) {
			return false
		}
	} else if capacity.Cmp(allocatable) != 0 {
		// Fallback when expectedChips is not set (e.g. unknown machine type)
		return false
	}

	return true
}
