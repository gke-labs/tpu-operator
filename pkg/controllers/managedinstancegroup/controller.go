package managedinstancegroup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
)

const finalizerName = "tpu.google.com/managedinstancegroup-cleanup"

// ManagedInstanceGroupReconciler reconciles a ManagedInstanceGroup object
type ManagedInstanceGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	GCE    gce.IGMClient
	GCEOps gce.ZoneOperationsClient
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups/finalizers,verbs=update

func (r *ManagedInstanceGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := r.Log.WithValues("managedinstancegroup", req.NamespacedName)
	logger.Info("Starting reconciliation")
	defer logger.Info("Done reconciliation")

	// Fetch the ManagedInstanceGroup instance
	var mig tpuv1alpha1.ManagedInstanceGroup
	if err := r.Get(ctx, req.NamespacedName, &mig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ManagedInstanceGroup not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ManagedInstanceGroup")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Successfully fetched ManagedInstanceGroup")

	// Status Patching Setup: DeepCopy the fetched object to use as a base for status patching.
	base := mig.DeepCopy()

	// Status Patching Defer: Add a deferred function to patch the status if changes were made, unless the object is fully deleted.
	defer func() {
		if !mig.DeletionTimestamp.IsZero() && len(mig.Finalizers) == 0 {
			return
		}
		if err := r.Status().Patch(ctx, &mig, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	// Finalizer Handling (Deletion)
	if !mig.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&mig, finalizerName) {
			logger.Info("Resource being deleted, removing finalizer")
			controllerutil.RemoveFinalizer(&mig, finalizerName)
			if err := r.Update(ctx, &mig); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Finalizer Handling (Creation/Update)
	if controllerutil.AddFinalizer(&mig, finalizerName) {
		logger.Info("Adding finalizer")
		if err := r.Update(ctx, &mig); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// Return placeholder for actual logic
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedInstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.ManagedInstanceGroup{}).
		Complete(r)
}
