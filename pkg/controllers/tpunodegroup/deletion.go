package tpunodegroup

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// cordonNodes taints all nodes associated with the TPUNodeGroup as NoSchedule.
func cordonNodes(ctx context.Context, logger logr.Logger, k8sClient client.Client, igmClient gce.IGMClient, group *tpuapi.TPUNodeGroup) error {
	// 1. List Node objects in the cluster with matching label
	var nodeList corev1.NodeList
	labelSelector := client.MatchingLabels{
		"cloud.google.com/tpu-node-group": fmt.Sprintf("%s-%s", group.Namespace, group.Name),
	}
	if err := k8sClient.List(ctx, &nodeList, labelSelector); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	// 2. Cordon nodes
	taint := corev1.Taint{
		Key:    corev1.TaintNodeUnschedulable,
		Effect: corev1.TaintEffectNoSchedule,
	}

	var errs []error
	for _, node := range nodeList.Items {
		if hasTaint(&node, taint.Key) {
			continue
		}

		logger.Info("Cordoning node", "node", node.Name)
		// Apply taint using Patch to avoid conflicts
		oldNode := node.DeepCopy()
		node.Spec.Taints = append(node.Spec.Taints, taint)

		if err := k8sClient.Patch(ctx, &node, client.MergeFrom(oldNode)); err != nil {
			errs = append(errs, fmt.Errorf("failed to patch node %s with taint: %w", node.Name, err))
		}
	}

	return errors.Join(errs...)
}

// hasTaint checks if a node already has a specific taint key.
func hasTaint(node *corev1.Node, key string) bool {
	for _, t := range node.Spec.Taints {
		if t.Key == key {
			return true
		}
	}
	return false
}

// deleteChildCRs deletes the child CRs in order: MIG, Template, Policy.
// It returns true if all resources are gone, and false if it needs to wait (requeue).
func deleteChildCRs(ctx context.Context, k8sClient client.Client, group *tpuapi.TPUNodeGroup) (bool, error) {
	// 1. Delete ManagedInstanceGroup
	migName := group.Name + "-mig"
	var mig tpuapi.ManagedInstanceGroup
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: migName}, &mig)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to get ManagedInstanceGroup: %w", err)
		}
		// MIG is gone, proceed to next
	} else {
		if mig.DeletionTimestamp.IsZero() {
			if err := k8sClient.Delete(ctx, &mig); err != nil {
				return false, fmt.Errorf("failed to delete ManagedInstanceGroup: %w", err)
			}
		}
		// Wait for it to be gone
		return false, nil
	}

	// 2. Delete InstanceTemplate
	templateName := group.Name + "-template"
	var template tpuapi.InstanceTemplate
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: templateName}, &template)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to get InstanceTemplate: %w", err)
		}
		// Template is gone, proceed to next
	} else {
		if template.DeletionTimestamp.IsZero() {
			if err := k8sClient.Delete(ctx, &template); err != nil {
				return false, fmt.Errorf("failed to delete InstanceTemplate: %w", err)
			}
		}
		// Wait for it to be gone
		return false, nil
	}

	// 3. Delete WorkloadPolicy
	policyName := group.Name + "-policy"
	var policy tpuapi.WorkloadPolicy
	err = k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: policyName}, &policy)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to get WorkloadPolicy: %w", err)
		}
		// Policy is gone, all done!
	} else {
		if policy.DeletionTimestamp.IsZero() {
			if err := k8sClient.Delete(ctx, &policy); err != nil {
				return false, fmt.Errorf("failed to delete WorkloadPolicy: %w", err)
			}
		}
		// Wait for it to be gone
		return false, nil
	}

	return true, nil
}

// TODO(b/512987019): Implement deleteNodeObjects
// func deleteNodeObjects(ctx context.Context, client client.Client, group *tpuapi.TPUNodeGroup) error {
// 	return nil
// }
