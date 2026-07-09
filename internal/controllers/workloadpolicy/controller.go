package workloadpolicy

import (
	"context"
	"fmt"
	"net/http"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/errorutil"
	"github.com/gke-labs/tpu-operator/internal/controllers/requeue"
	"github.com/gke-labs/tpu-operator/internal/converter"
	"github.com/gke-labs/tpu-operator/internal/gce"
)

// finalizerName is the name of the finalizer used to ensure clean teardown
// of external resources associated with an WorkloadPolicy.
const finalizerName = "tpu.google.com/workloadpolicy-cleanup"

// WorkloadPolicyReconciler reconciles a WorkloadPolicy object
type WorkloadPolicyReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	GCE      gce.ResourcePolicyClient
	GCEOps   gce.RegionOperationsClient
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=workloadpolicies/finalizers,verbs=update

func (r *WorkloadPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("reconciling WorkloadPolicy")

	// 1. Fetch the WorkloadPolicy instance
	var workloadPolicy tpuv1alpha1.WorkloadPolicy
	if err := r.Get(ctx, req.NamespacedName, &workloadPolicy); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("workloadPolicy not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get WorkloadPolicy: %w", err)
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
		logger.V(1).Info("checking pending GCE operation", "operation", workloadPolicy.Status.OperationName)
		opProto, err := r.GCEOps.Get(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, workloadPolicy.Status.OperationName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting GCE operation: %w", err)
		}

		if opProto.GetStatus() != computepb.Operation_DONE {
			logger.V(1).Info("GCE operation still pending", "operation", workloadPolicy.Status.OperationName)
			return ctrl.Result{RequeueAfter: requeue.Jittered(requeue.LROPollInterval)}, nil
		}

		// Operation is DONE
		if opProto.HttpErrorStatusCode != nil && (opProto.GetHttpErrorStatusCode() < 200 || opProto.GetHttpErrorStatusCode() > 299) {
			if !workloadPolicy.ObjectMeta.DeletionTimestamp.IsZero() && opProto.GetHttpErrorStatusCode() == http.StatusNotFound {
				logger.V(1).Info("ignoring 404 during deletion operation")
			} else {
				opName := workloadPolicy.Status.OperationName
				workloadPolicy.Status.OperationName = ""
				workloadPolicy.Status.OperationType = ""
				msg := fmt.Sprintf("GCE operation %q failed: %s (code %d): %v", opName, opProto.GetHttpErrorMessage(), opProto.GetHttpErrorStatusCode(), opProto.GetError())
				if errorutil.SetTerminalCondition(&workloadPolicy.Status.Conditions, tpuv1alpha1.ReasonOperationFailed, msg) {
					r.Recorder.Event(&workloadPolicy, corev1.EventTypeWarning, tpuv1alpha1.ReasonOperationFailed, msg)
				}
				return ctrl.Result{RequeueAfter: requeue.Jittered(requeue.DriftCheckInterval)}, nil
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
		logger.Info("workloadPolicy is being deleted")
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
				r.Recorder.Event(&workloadPolicy, corev1.EventTypeNormal, "Cleanup", fmt.Sprintf("GCE deletion operation started: %s", op.Name()))
				return ctrl.Result{RequeueAfter: requeue.Jittered(requeue.LROPollInterval)}, nil
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
			logger.Info("creating GCE ResourcePolicy", "name", workloadPolicy.Name)
			policy := converter.ToGCEResourcePolicy(&workloadPolicy)
			op, err := r.GCE.Insert(ctx, workloadPolicy.Spec.Project, workloadPolicy.Spec.Region, policy)
			if err != nil {
				classification := errorutil.Classify(err)
				if classification == errorutil.ClassificationTerminal {
					msg := fmt.Sprintf("GCP API rejected WorkloadPolicy creation: %v", err)
					if errorutil.SetTerminalCondition(&workloadPolicy.Status.Conditions, tpuv1alpha1.ReasonRequestRejected, msg) {
						r.Recorder.Event(&workloadPolicy, corev1.EventTypeWarning, tpuv1alpha1.ReasonRequestRejected, msg)
					}
					return ctrl.Result{RequeueAfter: requeue.Jittered(requeue.DriftCheckInterval)}, nil
				}
				return ctrl.Result{}, fmt.Errorf("inserting GCE ResourcePolicy: %w", err)
			}
			if op != nil {
				if !op.Done() {
					workloadPolicy.Status.OperationName = op.Name()
					workloadPolicy.Status.OperationType = "CREATE"
					logger.Info("GCE insert operation started", "operation", op.Name())
					r.Recorder.Event(&workloadPolicy, corev1.EventTypeNormal, "Provisioning", fmt.Sprintf("GCE creation operation started: %s", op.Name()))
					return ctrl.Result{RequeueAfter: requeue.Jittered(requeue.LROPollInterval)}, nil
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
	wasReady := meta.IsStatusConditionTrue(base.Status.Conditions, "Ready")
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
	if !wasReady {
		r.Recorder.Event(&workloadPolicy, corev1.EventTypeNormal, "Provisioned", fmt.Sprintf("GCE resource successfully created: %s", gcePolicy.GetSelfLink()))
	}

	logger.V(1).Info("reconciliation finished")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkloadPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("WorkloadPolicyController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.WorkloadPolicy{}).
		Complete(r)
}
