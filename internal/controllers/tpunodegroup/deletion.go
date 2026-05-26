package tpunodegroup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/tpunodegroup/deviceplugin"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const tpuNodeGroupLabel = "cloud.google.com/tpu-node-group"

// cordonNodes taints all nodes associated with the TPUNodeGroup as NoSchedule.
func cordonNodes(ctx context.Context, logger logr.Logger, k8sClient client.Client, group *tpuapi.TPUNodeGroup) error {
	// 1. List Node objects in the cluster with matching label
	var nodeList corev1.NodeList
	labelSelector := client.MatchingLabels{
		tpuNodeGroupLabel: fmt.Sprintf("%s-%s", group.Namespace, group.Name),
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

// ensureManagedInstanceGroupDeleted ensures the ManagedInstanceGroup is deleted.
// It returns true if the resource is gone, and false if it initiated deletion or is waiting.
func ensureManagedInstanceGroupDeleted(ctx context.Context, k8sClient client.Client, group *tpuapi.TPUNodeGroup) (bool, error) {
	migName := group.ManagedInstanceGroupName()
	var mig tpuapi.ManagedInstanceGroup
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: migName}, &mig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to get ManagedInstanceGroup: %w", err)
	}

	if mig.DeletionTimestamp.IsZero() {
		if err := k8sClient.Delete(ctx, &mig); err != nil {
			return false, fmt.Errorf("failed to delete ManagedInstanceGroup: %w", err)
		}
	}
	return false, nil
}

// ensureInstanceTemplateDeleted ensures the InstanceTemplate is deleted.
// It returns true if the resource is gone, and false if it initiated deletion or is waiting.
func ensureInstanceTemplateDeleted(ctx context.Context, k8sClient client.Client, group *tpuapi.TPUNodeGroup) (bool, error) {
	templateName := group.InstanceTemplateName()
	var template tpuapi.InstanceTemplate
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: templateName}, &template)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to get InstanceTemplate: %w", err)
	}

	if template.DeletionTimestamp.IsZero() {
		if err := k8sClient.Delete(ctx, &template); err != nil {
			return false, fmt.Errorf("failed to delete InstanceTemplate: %w", err)
		}
	}
	return false, nil
}

// ensureWorkloadPolicyDeleted ensures the WorkloadPolicy is deleted.
// It returns true if the resource is gone, and false if it initiated deletion or is waiting.
func ensureWorkloadPolicyDeleted(ctx context.Context, k8sClient client.Client, group *tpuapi.TPUNodeGroup) (bool, error) {
	policyName := group.WorkloadPolicyName()
	var policy tpuapi.WorkloadPolicy
	err := k8sClient.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: policyName}, &policy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("failed to get WorkloadPolicy: %w", err)
	}

	if policy.DeletionTimestamp.IsZero() {
		if err := k8sClient.Delete(ctx, &policy); err != nil {
			return false, fmt.Errorf("failed to delete WorkloadPolicy: %w", err)
		}
	}
	return false, nil
}

// deleteNodeObjects deletes Kubernetes Node objects that were associated with the group.
func deleteNodeObjects(ctx context.Context, logger logr.Logger, k8sClient client.Client, group *tpuapi.TPUNodeGroup) error {
	var nodeList corev1.NodeList
	labelSelector := client.MatchingLabels{
		"cloud.google.com/tpu-node-group": fmt.Sprintf("%s-%s", group.Namespace, group.Name),
	}
	if err := k8sClient.List(ctx, &nodeList, labelSelector); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	var errs []error
	for _, node := range nodeList.Items {
		logger.Info("Deleting stale Node object", "node", node.Name)
		if err := k8sClient.Delete(ctx, &node); err != nil {
			if !apierrors.IsNotFound(err) {
				errs = append(errs, fmt.Errorf("failed to delete node %s: %w", node.Name, err))
			}
		}
	}

	return errors.Join(errs...)
}

// cleanupDevicePluginIfLastGroup deletes the shared TPU Device Plugin DaemonSet
// if there are no other active TPUNodeGroups in the cluster.
func cleanupDevicePluginIfLastGroup(ctx context.Context, k8sClient client.Client, logger logr.Logger) (bool, error) {
	// 1. List all TPUNodeGroups
	var groupList tpuapi.TPUNodeGroupList
	if err := k8sClient.List(ctx, &groupList); err != nil {
		return false, fmt.Errorf("failed to list TPUNodeGroups: %w", err)
	}

	// 2. Check if there are any active groups (not being deleted)
	for _, g := range groupList.Items {
		if g.DeletionTimestamp.IsZero() {
			logger.Info("Skipping device plugin deletion as active TPUNodeGroups still exist")
			return true, nil
		}
	}

	// 3. Delete DaemonSet
	logger.Info("Deleting shared TPU Device Plugin DaemonSet (last group being deleted)")
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deviceplugin.DevicePluginName,
			Namespace: deviceplugin.DevicePluginNamespace,
		},
	}
	if err := k8sClient.Delete(ctx, ds); err != nil {
		if !apierrors.IsNotFound(err) {
			return false, fmt.Errorf("failed to delete device plugin DaemonSet: %w", err)
		}
	}

	return true, nil
}

// handleDeletion handles the deletion of the TPUNodeGroup and its child resources.
// It returns a ctrl.Result and an error. If the result is non-empty or error is non-nil,
// the caller should return immediately.
func handleDeletion(ctx context.Context, logger logr.Logger, k8sClient client.Client, group *tpuapi.TPUNodeGroup) (ctrl.Result, error) {
	if group.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	logger.Info("TPUNodeGroup is being deleted")

	hasAnyFinalizer := controllerutil.ContainsFinalizer(group, finalizerMIG) ||
		controllerutil.ContainsFinalizer(group, finalizerTemplate) ||
		controllerutil.ContainsFinalizer(group, finalizerPolicy) ||
		controllerutil.ContainsFinalizer(group, finalizerNodes) ||
		controllerutil.ContainsFinalizer(group, finalizerDevicePlugin)

	if !hasAnyFinalizer {
		return ctrl.Result{}, nil
	}

	logger.Info("Cordoning nodes")
	if err := cordonNodes(ctx, logger, k8sClient, group); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to cordon nodes: %w", err)
	}

	// Process finalizers sequentially

	// 1. MIG
	if controllerutil.ContainsFinalizer(group, finalizerMIG) {
		logger.Info("Ensuring ManagedInstanceGroup is deleted")
		done, err := ensureManagedInstanceGroupDeleted(ctx, k8sClient, group)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete MIG: %w", err)
		}
		if !done {
			logger.Info("Waiting for ManagedInstanceGroup to be deleted")
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    tpuapi.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  tpuapi.ReasonDeletingMIG,
				Message: "Waiting for ManagedInstanceGroup to be deleted",
			})
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		patchBase := group.DeepCopy()
		controllerutil.RemoveFinalizer(group, finalizerMIG)
		if err := k8sClient.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove MIG finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and reconcile again
	}

	// 2. Template
	if controllerutil.ContainsFinalizer(group, finalizerTemplate) {
		logger.Info("Ensuring InstanceTemplate is deleted")
		done, err := ensureInstanceTemplateDeleted(ctx, k8sClient, group)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete InstanceTemplate: %w", err)
		}
		if !done {
			logger.Info("Waiting for InstanceTemplate to be deleted")
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    tpuapi.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  tpuapi.ReasonDeletingTemplate,
				Message: "Waiting for InstanceTemplate to be deleted",
			})
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		patchBase := group.DeepCopy()
		controllerutil.RemoveFinalizer(group, finalizerTemplate)
		if err := k8sClient.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove Template finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and reconcile again
	}

	// 3. Policy
	if controllerutil.ContainsFinalizer(group, finalizerPolicy) {
		logger.Info("Ensuring WorkloadPolicy is deleted")
		done, err := ensureWorkloadPolicyDeleted(ctx, k8sClient, group)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete WorkloadPolicy: %w", err)
		}
		if !done {
			logger.Info("Waiting for WorkloadPolicy to be deleted")
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    tpuapi.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  tpuapi.ReasonDeletingPolicy,
				Message: "Waiting for WorkloadPolicy to be deleted",
			})
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		patchBase := group.DeepCopy()
		controllerutil.RemoveFinalizer(group, finalizerPolicy)
		if err := k8sClient.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove Policy finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and reconcile again
	}

	// 4. Nodes
	if controllerutil.ContainsFinalizer(group, finalizerNodes) {
		logger.Info("Deleting stale Node objects")
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    tpuapi.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  tpuapi.ReasonDeletingNodes,
			Message: "Deleting stale Node objects",
		})
		if err := deleteNodeObjects(ctx, logger, k8sClient, group); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete node objects: %w", err)
		}
		patchBase := group.DeepCopy()
		controllerutil.RemoveFinalizer(group, finalizerNodes)
		if err := k8sClient.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove Nodes finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and reconcile again
	}

	// 5. Device Plugin
	if controllerutil.ContainsFinalizer(group, finalizerDevicePlugin) {
		logger.Info("Ensuring TPU Device Plugin is deleted if last group")
		done, err := cleanupDevicePluginIfLastGroup(ctx, k8sClient, logger)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to delete TPU Device Plugin: %w", err)
		}
		if !done {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		patchBase := group.DeepCopy()
		controllerutil.RemoveFinalizer(group, finalizerDevicePlugin)
		if err := k8sClient.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to remove Device Plugin finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and reconcile again
	}

	return ctrl.Result{}, nil
}
