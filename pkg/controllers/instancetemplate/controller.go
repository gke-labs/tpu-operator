package instancetemplate

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
)

// finalizerName is the name of the finalizer used to ensure clean teardown
// of external resources associated with an InstanceTemplate.
const finalizerName = "tpu.google.com/instancetemplate-cleanup"

// InstanceTemplateReconciler reconciles a InstanceTemplate object
type InstanceTemplateReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Log      logr.Logger
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates/finalizers,verbs=update

func (r *InstanceTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("instancetemplate", req.NamespacedName)
	logger.Info("Reconciling InstanceTemplate")

	// 1. Fetch the InstanceTemplate instance
	var instanceTemplate tpuv1alpha1.InstanceTemplate
	if err := r.Get(ctx, req.NamespacedName, &instanceTemplate); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("InstanceTemplate not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get InstanceTemplate")
		return ctrl.Result{}, err
	}

	// 2. Check if the resource is being deleted
	if !instanceTemplate.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("InstanceTemplate is being deleted")
		if controllerutil.ContainsFinalizer(&instanceTemplate, finalizerName) {
			// TODO(b/500811406): Implement external cleanup (delete GCE Instance Template).
			// WARNING: Actual cleanup must be implemented before removing the finalizer once resource creation is added.

			controllerutil.RemoveFinalizer(&instanceTemplate, finalizerName)
			if err := r.Update(ctx, &instanceTemplate); err != nil {
				logger.Error(err, "Failed to remove finalizer from InstanceTemplate")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&instanceTemplate, finalizerName) {
		if err := r.Update(ctx, &instanceTemplate); err != nil {
			logger.Error(err, "Failed to add finalizer to InstanceTemplate")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// TODO: Implement actual reconciliation logic here

	logger.Info("Reconciliation finished")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *InstanceTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("InstanceTemplateController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.InstanceTemplate{}).
		Complete(r)
}
