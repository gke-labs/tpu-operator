package workloadpolicy

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
)

// WorkloadPolicyReconciler reconciles a WorkloadPolicy object
type WorkloadPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Log      logr.Logger
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies/finalizers,verbs=update

func (r *WorkloadPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := r.Log.WithValues("workloadpolicy", req.NamespacedName)
	logger.Info("Reconciling WorkloadPolicy")

	// 1. Fetch the WorkloadPolicy instance
	var workloadPolicy tpuv1alpha1.WorkloadPolicy
	if err := r.Get(ctx, req.NamespacedName, &workloadPolicy); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("WorkloadPolicy not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get WorkloadPolicy")
		return ctrl.Result{}, err
	}

	base := workloadPolicy.DeepCopy()
	defer func() {
		// Universal deletion guard: don't patch status if the object is fully deleted.
		if !workloadPolicy.DeletionTimestamp.IsZero() && len(workloadPolicy.Finalizers) == 0 {
			return
		}
		if err := r.Status().Patch(ctx, &workloadPolicy, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	// TODO: Implement WorkloadPolicy reconciliation logic.
	// This will involve creating/managing GCE Resource Policies (Workload Policies).

	logger.Info("Reconciliation finished")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkloadPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("WorkloadPolicyController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.WorkloadPolicy{}).
		Complete(r)
}
