package workloadpolicy

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
// of external resources associated with an WorkloadPolicy.
const finalizerName = "tpu.google.com/workloadpolicy-cleanup"

// WorkloadPolicyReconciler reconciles a WorkloadPolicy object
type WorkloadPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Log      logr.Logger
	GCE      gce.ResourcePolicyClient
	GCEOps   gce.RegionOperationsClient
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

	// 2. Check pending operation
	if workloadPolicy.Status.OperationName != "" {
		logger.Info("Checking pending GCE operation", "operation", workloadPolicy.Status.OperationName)
		opProto, err := r.GCEOps.Get(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, workloadPolicy.Status.OperationName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting GCE operation: %w", err)
		}

		if opProto.GetStatus() != computepb.Operation_DONE {
			logger.Info("GCE operation still pending", "operation", workloadPolicy.Status.OperationName)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Operation is DONE
		if opProto.HttpErrorStatusCode != nil && (opProto.GetHttpErrorStatusCode() < 200 || opProto.GetHttpErrorStatusCode() > 299) {
			if !workloadPolicy.ObjectMeta.DeletionTimestamp.IsZero() && opProto.GetHttpErrorStatusCode() == http.StatusNotFound {
				logger.Info("Ignoring 404 during deletion operation")
			} else {
				err := fmt.Errorf("GCE operation failed: %s (code %d): %v", opProto.GetHttpErrorMessage(), opProto.GetHttpErrorStatusCode(), opProto.GetError())
				logger.Error(err, "GCE operation failed", "operation", workloadPolicy.Status.OperationName)
				workloadPolicy.Status.OperationName = ""
				return ctrl.Result{}, err
			}
		} else {
			logger.Info("GCE operation completed successfully", "operation", workloadPolicy.Status.OperationName)
		}

		if workloadPolicy.Status.OperationType == "DELETE" {
			// Deletion operation completed successfully, remove finalizer
			if controllerutil.ContainsFinalizer(&workloadPolicy, finalizerName) {
				controllerutil.RemoveFinalizer(&workloadPolicy, finalizerName)
				if err := r.Update(ctx, &workloadPolicy); err != nil {
					return ctrl.Result{}, fmt.Errorf("removing finalizer from WorkloadPolicy after GCE delete: %w", err)
				}
			}
			return ctrl.Result{}, nil
		}

		if workloadPolicy.Status.OperationType == "CREATE" {
			// Insert operation completed successfully. Clear operation name and type and requeue to refetch template.
			workloadPolicy.Status.OperationName = ""
			workloadPolicy.Status.OperationType = ""
			return ctrl.Result{Requeue: true}, nil
		}

		// Fallback for resources created before OperationType was introduced.
		if !workloadPolicy.ObjectMeta.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(&workloadPolicy, finalizerName) {
				controllerutil.RemoveFinalizer(&workloadPolicy, finalizerName)
				if err := r.Update(ctx, &workloadPolicy); err != nil {
					return ctrl.Result{}, fmt.Errorf("removing finalizer from WorkloadPolicy after GCE delete: %w", err)
				}
			}
			return ctrl.Result{}, nil
		}
		workloadPolicy.Status.OperationName = ""
		return ctrl.Result{Requeue: true}, nil
	}

	// 3. Check if the resource is being deleted
	if !workloadPolicy.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("WorkloadPolicy is being deleted")
		if controllerutil.ContainsFinalizer(&workloadPolicy, finalizerName) {
			// Delete GCE Resource Policy
			op, err := r.GCE.Delete(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, workloadPolicy.Name)
			if err != nil {
				if gce.IsNotFound(err) {
					// Already deleted, remove finalizer
					controllerutil.RemoveFinalizer(&workloadPolicy, finalizerName)
					if err := r.Update(ctx, &workloadPolicy); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from WorkloadPolicy: %w", err)
					}
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, fmt.Errorf("deleting GCE ResourcePolicy: %w", err)
			}
			if op != nil {
				if op.Done() {
					controllerutil.RemoveFinalizer(&workloadPolicy, finalizerName)
					if err := r.Update(ctx, &workloadPolicy); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from WorkloadPolicy: %w", err)
					}
					return ctrl.Result{}, nil
				}
				workloadPolicy.Status.OperationName = op.Name()
				workloadPolicy.Status.OperationType = "DELETE"
				logger.Info("GCE delete operation started", "operation", op.Name())
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			controllerutil.RemoveFinalizer(&workloadPolicy, finalizerName)
			if err := r.Update(ctx, &workloadPolicy); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer from WorkloadPolicy: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&workloadPolicy, finalizerName) {
		if err := r.Update(ctx, &workloadPolicy); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer to WorkloadPolicy: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure GCE Resource Policy exists
	gcePolicy, err := r.GCE.Get(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, workloadPolicy.Name)
	if err != nil {
		if gce.IsNotFound(err) {
			logger.Info("Creating GCE ResourcePolicy", "name", workloadPolicy.Name)
			policy := converter.ToGCEResourcePolicy(&workloadPolicy)
			op, err := r.GCE.Insert(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, policy)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("inserting GCE ResourcePolicy: %w", err)
			}
			if op != nil {
				if !op.Done() {
					workloadPolicy.Status.OperationName = op.Name()
					workloadPolicy.Status.OperationType = "CREATE"
					logger.Info("GCE insert operation started", "operation", op.Name())
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			}

			// Refetch to get the URI
			gcePolicy, err = r.GCE.Get(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, workloadPolicy.Name)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("getting GCE ResourcePolicy after creation: %w", err)
			}
		} else {
			return ctrl.Result{}, fmt.Errorf("getting GCE ResourcePolicy: %w", err)
		}
	}

	// 5. Update Status
	workloadPolicy.Status.URI = gcePolicy.GetSelfLink()
	workloadPolicy.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Created",
			Message:            "GCE Resource Policy successfully created",
			LastTransitionTime: metav1.Now(),
		},
	}

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
