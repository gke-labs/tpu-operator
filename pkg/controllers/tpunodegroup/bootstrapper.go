package tpunodegroup

import (
	"context"
	"fmt"
	"strings"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeBootstrapper handles node bootstrapping logic.
type NodeBootstrapper struct {
	client client.Client
	igm    gce.IGMClient
}

// NewNodeBootstrapper creates a new NodeBootstrapper.
func NewNodeBootstrapper(client client.Client, igm gce.IGMClient) *NodeBootstrapper {
	return &NodeBootstrapper{
		client: client,
		igm:    igm,
	}
}

// ReconcileNodeJoin checks if nodes have joined the cluster and updates status.
func (b *NodeBootstrapper) ReconcileNodeJoin(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	base := group.DeepCopy()

	// 1. Get list of expected instances from MIG
	// Convention: migName = group.Name for now.
	// TODO(b/500810349): Get actual MIG name from status or child CR when available.
	migName := group.Name

	instances, err := b.igm.ListManagedInstances(ctx, group.Spec.Project, group.Spec.NodeLocation, migName)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	// 2. List Node objects in the cluster
	var nodeList corev1.NodeList
	if err := b.client.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// 3. Match Nodes to expected instances
	nodeNames := make(map[string]bool)
	for _, inst := range instances {
		// inst.GetInstance() returns the full URL of the instance.
		// We extract the name (last part) to match against Node name.
		name := instanceShortName(inst.GetInstance())
		if name != "" {
			nodeNames[name] = true
		}
	}

	readyCount := 0

	for _, node := range nodeList.Items {
		// Match by name as requested
		if nodeNames[node.Name] {
			// Check if node is ready
			for _, cond := range node.Status.Conditions {
				if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
	}

	// 4. Update TPUNodeGroup status
	if group.Status.NodeSummary == nil {
		group.Status.NodeSummary = &tpuapi.NodeSummary{}
	}
	group.Status.NodeSummary.Total = group.Spec.NodeCount
	group.Status.NodeSummary.Ready = int32(readyCount)
	// For now, reconciling means expected but not ready.
	group.Status.NodeSummary.Reconciling = group.Spec.NodeCount - int32(readyCount)

	// Persist status update
	if err := b.client.Status().Patch(ctx, group, client.MergeFrom(base)); err != nil {
		return fmt.Errorf("failed to patch TPUNodeGroup status: %w", err)
	}

	// TODO(b/500810349): Use providerID for lookup in the future.

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
