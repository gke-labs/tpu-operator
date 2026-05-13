package tpunodegroup

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/tpunodegroup/deviceplugin"
	"gke-internal.googlesource.com/tpu-node-group/pkg/converter"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	"k8s.io/utils/ptr"
)

// TPUNodeGroupReconciler reconciles a TPUNodeGroup object.
type TPUNodeGroupReconciler struct {
	client.Client
	scheme         *runtime.Scheme
	recorder       record.EventRecorder
	igmClient      gce.IGMClient
	instanceClient gce.InstanceClient
	kubeClientset  kubernetes.Interface
	Log            logr.Logger
}

// NewTPUNodeGroupReconciler creates a new TPUNodeGroupReconciler.
func NewTPUNodeGroupReconciler(client client.Client, scheme *runtime.Scheme, kubeClientset kubernetes.Interface, igmClient gce.IGMClient, instanceClient gce.InstanceClient, log logr.Logger) *TPUNodeGroupReconciler {
	return &TPUNodeGroupReconciler{
		Client:         client,
		scheme:         scheme,
		kubeClientset:  kubeClientset,
		igmClient:      igmClient,
		instanceClient: instanceClient,
		Log:            log,
	}
}

// WithRecorder sets the event recorder for the reconciler (useful for testing).
func (r *TPUNodeGroupReconciler) WithRecorder(recorder record.EventRecorder) *TPUNodeGroupReconciler {
	r.recorder = recorder
	return r
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=tpunodegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tpu.google.com,resources=tpunodegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tpu.google.com,resources=tpunodegroups/finalizers,verbs=update

// Reconcile is the main entry point for reconciling a TPUNodeGroup.
//
// ARCHITECTURAL NOTICE ON STATUS PATCHING:
// Sub-reconcilers (e.g., reconcileInstanceTemplate, ReconcileNodeJoin) must act
// strictly as in-memory mutators of the TPUNodeGroup struct. They should never issue
// intermediate Status().Patch() calls. Intermediate patches overwrite the in-memory object
// with server state, wiping out unpersisted condition changes. All cumulative state
// updates are persisted atomically by the single Status().Patch() call at the end of Reconcile.
func (r *TPUNodeGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := r.Log.WithValues("tpunodegroup", req.NamespacedName)

	// 1. Fetch the TPUNodeGroup resource
	var tpuNodeGroup tpuapi.TPUNodeGroup
	if err := r.Get(ctx, req.NamespacedName, &tpuNodeGroup); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("TPUNodeGroup no longer exists")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	base := tpuNodeGroup.DeepCopy()
	defer func() {
		// TODO(b/500810949): handle the case where TPUNodeGroup is about to be deleted
		// after the deletion of all of the k8s resources it owns.
		if err := r.Status().Patch(ctx, &tpuNodeGroup, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	logger.Info("Reconciling TPUNodeGroup")

	// Step 1: Reconcile Resource Policy
	if err := r.reconcileResourcePolicy(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile resource policy: %w", err)
	}

	// Step 2: Reconcile Instance Template
	if err := r.reconcileInstanceTemplate(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile instance template: %w", err)
	}

	// Step 3: Reconcile Managed Instance Group
	if err := r.reconcileManagedInstanceGroup(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile MIG: %w", err)
	}

	// Step 4: Reconcile Node Bootstrapping
	if err := r.reconcileNodeBootstrapping(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile node bootstrapping: %w", err)
	}

	// Step 5: Reconcile Device Plugin
	if err := deviceplugin.Reconcile(ctx, r.kubeClientset, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile device plugin: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *TPUNodeGroupReconciler) reconcileResourcePolicy(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement ResourcePolicy reconciliation (Composite Pattern).
	// Check if multi-host and if policy exists, create if not.
	return nil
}

func (r *TPUNodeGroupReconciler) reconcileInstanceTemplate(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	template := converter.ToInstanceTemplateCR(group)
	if template == nil {
		return nil
	}

	r.defaultInstanceTemplate(template)

	// TODO: Implement InstanceTemplate reconciliation (Create/Patch in K8s).
	return nil
}

func (r *TPUNodeGroupReconciler) reconcileManagedInstanceGroup(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement MIG reconciliation.
	// Create MIG in bulk mode referencing policy and template.
	return nil
}

func (r *TPUNodeGroupReconciler) reconcileNodeBootstrapping(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	bootstrapper := NewNodeBootstrapper(r.Client, r.igmClient)
	return bootstrapper.ReconcileNodeJoin(ctx, group)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TPUNodeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("TPUNodeGroupController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuapi.TPUNodeGroup{}).
		Complete(r)
}

// defaultInstanceTemplate populates default values for an InstanceTemplate prior to reconciliation.
func (r *TPUNodeGroupReconciler) defaultInstanceTemplate(template *tpuapi.InstanceTemplate) {
	if template == nil {
		return
	}
	if template.Spec.InstanceConfig.ProvisioningModel == nil {
		if template.Spec.InstanceConfig.Reservation != nil {
			template.Spec.InstanceConfig.ProvisioningModel = ptr.To("RESERVATION_BOUND")
		} else {
			template.Spec.InstanceConfig.ProvisioningModel = ptr.To("STANDARD")
		}
	}
}
