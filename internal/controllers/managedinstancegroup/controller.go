package managedinstancegroup

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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/converter"
	"github.com/gke-labs/tpu-operator/internal/gce"
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
	logger.Info("Reconciling ManagedInstanceGroup")

	// 1. Fetch the ManagedInstanceGroup instance
	var mig tpuv1alpha1.ManagedInstanceGroup
	if err := r.Get(ctx, req.NamespacedName, &mig); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ManagedInstanceGroup not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ManagedInstanceGroup")
		return ctrl.Result{}, err
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
		logger.Info("Checking pending GCE operation", "operation", mig.Status.OperationName)
		opProto, err := r.GCEOps.Get(ctx, mig.Spec.Project, mig.Spec.Location, mig.Status.OperationName)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting GCE operation: %w", err)
		}

		if opProto.GetStatus() != computepb.Operation_DONE {
			logger.Info("GCE operation still pending", "operation", mig.Status.OperationName)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// Operation is DONE
		if opProto.HttpErrorStatusCode != nil && (opProto.GetHttpErrorStatusCode() < 200 || opProto.GetHttpErrorStatusCode() > 299) {
			if !mig.ObjectMeta.DeletionTimestamp.IsZero() && opProto.GetHttpErrorStatusCode() == http.StatusNotFound {
				logger.Info("Ignoring 404 during deletion operation")
			} else {
				err := fmt.Errorf("GCE operation failed: %s (code %d): %v", opProto.GetHttpErrorMessage(), opProto.GetHttpErrorStatusCode(), opProto.GetError())
				logger.Error(err, "GCE operation failed", "operation", mig.Status.OperationName)
				mig.Status.OperationName = ""
				mig.Status.OperationType = ""
				return ctrl.Result{}, err
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
		logger.Info("ManagedInstanceGroup is being deleted")
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
			logger.Info("Creating GCE ManagedInstanceGroup", "name", mig.Name)
			template := converter.ToGCEManagedInstanceGroup(&mig)
			op, err := r.GCE.Insert(ctx, mig.Spec.Project, mig.Spec.Location, template)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("inserting GCE ManagedInstanceGroup: %w", err)
			}
			if op != nil {
				if !op.Done() {
					mig.Status.OperationName = op.Name()
					mig.Status.OperationType = "CREATE"
					logger.Info("GCE insert operation started", "operation", op.Name())
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
	mig.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Created",
			Message:            "GCE Managed Instance Group successfully created",
			LastTransitionTime: metav1.Now(),
		},
	}

	logger.Info("Reconciliation finished")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedInstanceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuv1alpha1.ManagedInstanceGroup{}).
		Complete(r)
}
