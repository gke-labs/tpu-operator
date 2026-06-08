package managedinstancegroup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	"github.com/gke-labs/tpu-operator/internal/converter"
	"github.com/gke-labs/tpu-operator/internal/gce"
)

const finalizerName = "tpu.google.com/managedinstancegroup-cleanup"

// ManagedInstanceGroupReconciler reconciles a ManagedInstanceGroup object
type ManagedInstanceGroupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	GCE      gce.IGMClient
	GCEOps   gce.ZoneOperationsClient
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=managedinstancegroups/finalizers,verbs=update

func (r *ManagedInstanceGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("reconciling ManagedInstanceGroup")

	// 1. Fetch the ManagedInstanceGroup instance
	var mig tpuv1alpha1.ManagedInstanceGroup
	if err := r.Get(ctx, req.NamespacedName, &mig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("managedInstanceGroup not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get ManagedInstanceGroup: %w", err)
	}

	base := mig.DeepCopy()
	defer func() {
		// Universal deletion guard: don't patch status if the object is fully deleted.
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

	// 2. Check pending operation
	if mig.Status.OperationName != "" {
		logger.V(1).Info("checking pending GCE operation", "operation", mig.Status.OperationName)
		opProto, err := r.GCEOps.Get(ctx, mig.Spec.Project, mig.Spec.Location, mig.Status.OperationName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting GCE operation: %w", err)
		}

		if opProto.GetStatus() != computepb.Operation_DONE {
			logger.V(1).Info("GCE operation still pending", "operation", mig.Status.OperationName)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Operation is DONE
		if opProto.HttpErrorStatusCode != nil && (opProto.GetHttpErrorStatusCode() < 200 || opProto.GetHttpErrorStatusCode() > 299) {
			if !mig.ObjectMeta.DeletionTimestamp.IsZero() && opProto.GetHttpErrorStatusCode() == http.StatusNotFound {
				logger.V(1).Info("ignoring 404 during deletion operation")
			} else {
				opName := mig.Status.OperationName
				mig.Status.OperationName = ""
				mig.Status.OperationType = ""
				msg := fmt.Sprintf("GCE operation %q failed: %s (code %d): %v", opName, opProto.GetHttpErrorMessage(), opProto.GetHttpErrorStatusCode(), opProto.GetError())
				if errorutil.SetTerminalCondition(&mig.Status.Conditions, tpuv1alpha1.ReasonOperationFailed, msg) {
					r.Recorder.Event(&mig, corev1.EventTypeWarning, tpuv1alpha1.ReasonOperationFailed, msg)
				}
				return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
			}
		} else {
			logger.Info("GCE operation completed successfully", "operation", mig.Status.OperationName)
		}

		if mig.Status.OperationType == "DELETE" {
			// Deletion operation completed successfully, remove finalizer
			if controllerutil.ContainsFinalizer(&mig, finalizerName) {
				controllerutil.RemoveFinalizer(&mig, finalizerName)
				if err := r.Update(ctx, &mig); err != nil {
					return ctrl.Result{}, fmt.Errorf("removing finalizer from ManagedInstanceGroup after GCE delete: %w", err)
				}
			}
			return ctrl.Result{}, nil
		}

		if mig.Status.OperationType == "CREATE" {
			// Insert operation completed successfully. Clear operation name and type and requeue to refetch template.
			mig.Status.OperationName = ""
			mig.Status.OperationType = ""
			return ctrl.Result{Requeue: true}, nil
		}

		// Fallback for resources created before OperationType was introduced.
		if !mig.ObjectMeta.DeletionTimestamp.IsZero() {
			if controllerutil.ContainsFinalizer(&mig, finalizerName) {
				controllerutil.RemoveFinalizer(&mig, finalizerName)
				if err := r.Update(ctx, &mig); err != nil {
					return ctrl.Result{}, fmt.Errorf("removing finalizer from ManagedInstanceGroup after GCE delete: %w", err)
				}
			}
			return ctrl.Result{}, nil
		}
		mig.Status.OperationName = ""
		return ctrl.Result{Requeue: true}, nil
	}

	// 3. Check if the resource is being deleted
	if !mig.ObjectMeta.DeletionTimestamp.IsZero() {
		logger.Info("managedInstanceGroup is being deleted")
		if controllerutil.ContainsFinalizer(&mig, finalizerName) {
			// Delete GCE MIG
			op, err := r.GCE.Delete(ctx, mig.Spec.Project, mig.Spec.Location, mig.Name)
			if err != nil {
				if gce.IsNotFound(err) {
					// Already deleted, remove finalizer
					controllerutil.RemoveFinalizer(&mig, finalizerName)
					if err := r.Update(ctx, &mig); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from ManagedInstanceGroup: %w", err)
					}
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, fmt.Errorf("deleting GCE ManagedInstanceGroup: %w", err)
			}
			if op != nil {
				if op.Done() {
					controllerutil.RemoveFinalizer(&mig, finalizerName)
					if err := r.Update(ctx, &mig); err != nil {
						return ctrl.Result{}, fmt.Errorf("removing finalizer from ManagedInstanceGroup: %w", err)
					}
					return ctrl.Result{}, nil
				}
				mig.Status.OperationName = op.Name()
				mig.Status.OperationType = "DELETE"
				logger.Info("GCE delete operation started", "operation", op.Name())
				r.Recorder.Event(&mig, corev1.EventTypeNormal, "Cleanup", fmt.Sprintf("GCE deletion operation started: %s", op.Name()))
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			controllerutil.RemoveFinalizer(&mig, finalizerName)
			if err := r.Update(ctx, &mig); err != nil {
				return ctrl.Result{}, fmt.Errorf("removing finalizer from ManagedInstanceGroup: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&mig, finalizerName) {
		if err := r.Update(ctx, &mig); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer to ManagedInstanceGroup: %w", err)
		}
		return ctrl.Result{}, nil
	}

	// 4. Ensure GCE Managed Instance Group exists
	gceMIG, err := r.GCE.Get(ctx, mig.Spec.Project, mig.Spec.Location, mig.Name)
	if err != nil {
		if gce.IsNotFound(err) {
			logger.Info("creating GCE ManagedInstanceGroup", "name", mig.Name)
			template := converter.ToGCEManagedInstanceGroup(&mig)
			op, err := r.GCE.Insert(ctx, mig.Spec.Project, mig.Spec.Location, template)
			if err != nil {
				classification := errorutil.Classify(err)
				if classification == errorutil.ClassificationTerminal {
					msg := fmt.Sprintf("GCP API rejected ManagedInstanceGroup creation: %v", err)
					if errorutil.SetTerminalCondition(&mig.Status.Conditions, tpuv1alpha1.ReasonRequestRejected, msg) {
						r.Recorder.Event(&mig, corev1.EventTypeWarning, tpuv1alpha1.ReasonRequestRejected, msg)
					}
					return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
				}
				return ctrl.Result{}, fmt.Errorf("inserting GCE ManagedInstanceGroup: %w", err)
			}
			if op != nil {
				if !op.Done() {
					mig.Status.OperationName = op.Name()
					mig.Status.OperationType = "CREATE"
					logger.Info("GCE insert operation started", "operation", op.Name())
					r.Recorder.Event(&mig, corev1.EventTypeNormal, "Provisioning", fmt.Sprintf("GCE creation operation started: %s", op.Name()))
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			}

			// Refetch to get the URL
			gceMIG, err = r.GCE.Get(ctx, mig.Spec.Project, mig.Spec.Location, mig.Name)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("getting GCE ManagedInstanceGroup after creation: %w", err)
			}
		} else {
			return ctrl.Result{}, fmt.Errorf("getting GCE ManagedInstanceGroup: %w", err)
		}
	}

	// 5. Update Status
	mig.Status.URL = gceMIG.GetSelfLink()

	// Check for instance creation failures (Phase 2)
	if gceMIG.Status != nil && gceMIG.Status.IsStable != nil && !*gceMIG.Status.IsStable {
		instances, err := r.GCE.ListManagedInstances(ctx, mig.Spec.Project, mig.Spec.Location, mig.Name)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("listing GCE managed instances: %w", err)
		}
		for _, ins := range instances {
			if ins.LastAttempt != nil && ins.LastAttempt.Errors != nil && len(ins.LastAttempt.Errors.Errors) > 0 {
				var msgs []string
				for _, e := range ins.LastAttempt.Errors.Errors {
					msgs = append(msgs, fmt.Sprintf("%s: %s", e.GetCode(), e.GetMessage()))
				}
				msg := fmt.Sprintf("MIG failed to create instances: %s", strings.Join(msgs, "; "))
				if errorutil.SetTerminalCondition(&mig.Status.Conditions, tpuv1alpha1.ReasonInstancesCreationFailed, msg) {
					r.Recorder.Event(&mig, corev1.EventTypeWarning, tpuv1alpha1.ReasonInstancesCreationFailed, msg)
				}
				break
			}
		}
	}

	if errorutil.TerminalFailureCondition(mig.Status.Conditions) != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
	}

	if gceMIG.Status != nil && gceMIG.Status.IsStable != nil && !*gceMIG.Status.IsStable {
		logger.V(1).Info("GCE Managed Instance Group is not yet stable, triggering exponential backoff retry")
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "Unstable",
			Message: "Waiting for GCE Managed Instance Group to become stable",
		})
		// Return an error to use exponential backoff provided by controller-runtime
		// to handle transient errors.
		// TODO: implemented customized solutions for exponential backoff for better
		// flexibility.
		return ctrl.Result{}, fmt.Errorf("waiting for GCE Managed Instance Group to become stable")
	}

	wasReady := meta.IsStatusConditionTrue(base.Status.Conditions, "Ready")
	mig.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Created",
			Message:            "GCE Managed Instance Group successfully created",
			LastTransitionTime: metav1.Now(),
		},
	}
	if !wasReady {
		r.Recorder.Event(&mig, corev1.EventTypeNormal, "Provisioned", fmt.Sprintf("GCE resource successfully created: %s", gceMIG.GetSelfLink()))
	}

	logger.V(1).Info("reconciliation finished")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedInstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Recorder = mgr.GetEventRecorderFor("ManagedInstanceGroupController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.ManagedInstanceGroup{}).
		Complete(r)
}
