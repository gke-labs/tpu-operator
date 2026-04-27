package tpunodegroup

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	clientset "gke-internal.googlesource.com/tpu-node-group/pkg/generated/clientset/versioned"
	listers "gke-internal.googlesource.com/tpu-node-group/pkg/generated/listers/tpu/v1alpha1"
)

// Reconciler handles the business logic of converging desired state to actual state.
type Reconciler struct {
	tpuClientset       clientset.Interface
	tpuNodeGroupLister listers.TPUNodeGroupLister
}

// NewReconciler returns a new Reconciler.
func NewReconciler(tpuClientset clientset.Interface, tpuNodeGroupLister listers.TPUNodeGroupLister) *Reconciler {
	return &Reconciler{
		tpuClientset:       tpuClientset,
		tpuNodeGroupLister: tpuNodeGroupLister,
	}
}

// Reconcile is the main entry point for reconciling a TPUNodeGroup.
func (r *Reconciler) Reconcile(ctx context.Context, objectRef cache.ObjectName) error {
	logger := klog.LoggerWithValues(klog.FromContext(ctx), "objectRef", objectRef)

	// 1. Fetch the TPUNodeGroup resource
	tpuNodeGroup, err := r.tpuNodeGroupLister.TPUNodeGroups(objectRef.Namespace).Get(objectRef.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TPUNodeGroup no longer exists")
			return nil
		}
		return err
	}

	logger.Info("Reconciling TPUNodeGroup")

	// Step 1: Reconcile Resource Policy
	if err := r.reconcileResourcePolicy(ctx, tpuNodeGroup); err != nil {
		return fmt.Errorf("failed to reconcile resource policy: %w", err)
	}

	// Step 2: Reconcile Instance Template
	if err := r.reconcileInstanceTemplate(ctx, tpuNodeGroup); err != nil {
		return fmt.Errorf("failed to reconcile instance template: %w", err)
	}

	// Step 3: Reconcile Managed Instance Group
	if err := r.reconcileManagedInstanceGroup(ctx, tpuNodeGroup); err != nil {
		return fmt.Errorf("failed to reconcile MIG: %w", err)
	}

	// Step 4: Reconcile Node Bootstrapping
	if err := r.reconcileNodeBootstrapping(ctx, tpuNodeGroup); err != nil {
		return fmt.Errorf("failed to reconcile node bootstrapping: %w", err)
	}

	return nil
}

func (r *Reconciler) reconcileResourcePolicy(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement ResourcePolicy reconciliation (Composite Pattern).
	// Check if multi-host and if policy exists, create if not.
	return nil
}

func (r *Reconciler) reconcileInstanceTemplate(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement InstanceTemplate reconciliation.
	// Check if user provided or if we need to create one.
	return nil
}

func (r *Reconciler) reconcileManagedInstanceGroup(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement MIG reconciliation.
	// Create MIG in bulk mode referencing policy and template.
	return nil
}

func (r *Reconciler) reconcileNodeBootstrapping(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement Node Bootstrapping check.
	// Watch nodes, check for ready status, and update TPUNodeGroup status.
	return nil
}
