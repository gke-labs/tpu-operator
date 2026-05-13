package instancetemplate

import (
	"context"
	"fmt"
	"net/http"
	"time"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/converter"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
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
	GCE      gce.InstanceTemplateClient
	GCEOps   gce.GlobalOperationsClient
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=instancetemplates/finalizers,verbs=update

func (r *InstanceTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
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

	base := instanceTemplate.DeepCopy()
	defer func() {
		// Universal deletion guard: don't patch status if the object is fully deleted.
		if !instanceTemplate.DeletionTimestamp.IsZero() && len(instanceTemplate.Finalizers) == 0 {
			return
		}
		if err := r.Status().Patch(ctx, &instanceTemplate, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	// 2. Check pending operation
	if instanceTemplate.Status.OperationName != "" {
		logger.Info("Checking pending GCE operation", "operation", instanceTemplate.Status.OperationName)
		opProto, err := r.GCEOps.Get(ctx, instanceTemplate.Spec.Project, instanceTemplate.Status.OperationName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting GCE operation: %w", err)
		}

		if opProto.GetStatus() != computepb.Operation_DONE {
			logger.Info("GCE operation still pending", "operation", instanceTemplate.Status.OperationName)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Operation is DONE
		if opProto.HttpErrorStatusCode != nil && (opProto.GetHttpErrorStatusCode() < 200 || opProto.GetHttpErrorStatusCode() > 299) {
			if !instanceTemplate.ObjectMeta.DeletionTimestamp.IsZero() && opProto.GetHttpErrorStatusCode() == http.StatusNotFound {
				logger.Info("Ignoring 404 during deletion operation")
			} else {
				err := fmt.Errorf("GCE operation failed: %s (code %d): %v", opProto.GetHttpErrorMessage(), opProto.GetHttpErrorStatusCode(), opProto.GetError())
				logger.Error(err, "GCE operation failed", "operation", instanceTemplate.Status.OperationName)
				return ctrl.Result{}, err
			}
		} else {
			logger.Info("GCE operation completed successfully", "operation", instanceTemplate.Status.OperationName)
		}

		if !instanceTemplate.ObjectMeta.DeletionTimestamp.IsZero() {
			// Deletion operation completed successfully, remove finalizer
			if controllerutil.ContainsFinalizer(&instanceTemplate, finalizerName) {
				controllerutil.RemoveFinalizer(&instanceTemplate, finalizerName)
				if err := r.Update(ctx, &instanceTemplate); err != nil {
					return ctrl.Result{}, fmt.Errorf("removing finalizer from InstanceTemplate after GCE delete: %w", err)
				}
			}
			return ctrl.Result{}, nil
		}

		// Insert operation completed successfully. Clear operation name and requeue to refetch template.
		instanceTemplate.Status.OperationName = ""
		return ctrl.Result{Requeue: true}, nil
	}

	// 3. Check if the resource is being deleted
	if !instanceTemplate.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("InstanceTemplate is being deleted")
		if controllerutil.ContainsFinalizer(&instanceTemplate, finalizerName) {
			// Delete GCE Instance Template
			op, err := r.GCE.Delete(ctx, instanceTemplate.Spec.Project, instanceTemplate.Name)
			if err != nil {
				if gce.IsNotFound(err) {
					// Already deleted, remove finalizer
					controllerutil.RemoveFinalizer(&instanceTemplate, finalizerName)
					if err := r.Update(ctx, &instanceTemplate); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from InstanceTemplate: %w", err)
					}
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, fmt.Errorf("deleting GCE InstanceTemplate: %w", err)
			}
			if op != nil {
				if op.Done() {
					controllerutil.RemoveFinalizer(&instanceTemplate, finalizerName)
					if err := r.Update(ctx, &instanceTemplate); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from InstanceTemplate: %w", err)
					}
					return ctrl.Result{}, nil
				}
				instanceTemplate.Status.OperationName = op.Name()
				logger.Info("GCE delete operation started", "operation", op.Name())
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			controllerutil.RemoveFinalizer(&instanceTemplate, finalizerName)
			if err := r.Update(ctx, &instanceTemplate); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer from InstanceTemplate: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&instanceTemplate, finalizerName) {
		if err := r.Update(ctx, &instanceTemplate); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer to InstanceTemplate: %w", err)
		}
		return ctrl.Result{}, nil
	}


	// 4. Ensure GCE Instance Template exists
	gceTemplate, err := r.GCE.Get(ctx, instanceTemplate.Spec.Project, instanceTemplate.Name)
	if err != nil {
		if gce.IsNotFound(err) {
			logger.Info("Creating GCE InstanceTemplate", "name", instanceTemplate.Name)
			template := converter.ToGCEInstanceTemplate(&instanceTemplate)
			op, err := r.GCE.Insert(ctx, instanceTemplate.Spec.Project, template)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("inserting GCE InstanceTemplate: %w", err)
			}
			if op != nil {
				if !op.Done() {
					instanceTemplate.Status.OperationName = op.Name()
					logger.Info("GCE insert operation started", "operation", op.Name())
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			}

			// Refetch to get the URI
			gceTemplate, err = r.GCE.Get(ctx, instanceTemplate.Spec.Project, instanceTemplate.Name)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("getting GCE InstanceTemplate after creation: %w", err)
			}
		} else {
			return ctrl.Result{}, fmt.Errorf("getting GCE InstanceTemplate: %w", err)
		}
	}

	// 5. Update Status

	instanceTemplate.Status.URI = gceTemplate.GetSelfLink()
	instanceTemplate.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Created",
			Message:            "GCE Instance Template successfully created",
			LastTransitionTime: metav1.Now(),
		},
	}



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
