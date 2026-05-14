package tpunodegroup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/controllers/tpunodegroup/deviceplugin"
	"gke-internal.googlesource.com/tpu-node-group/pkg/converter"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	"k8s.io/utils/ptr"
)

const finalizerName = "tpu.google.com/slice-cleanup"

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
		if apierrors.IsNotFound(err) {
			logger.Info("TPUNodeGroup no longer exists")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	base := tpuNodeGroup.DeepCopy()
	defer func() {
		// Universal deletion guard: don't patch status if the object is fully deleted.
		if !tpuNodeGroup.DeletionTimestamp.IsZero() && len(tpuNodeGroup.Finalizers) == 0 {
			return
		}
		if err := r.Status().Patch(ctx, &tpuNodeGroup, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	logger.Info("Reconciling TPUNodeGroup")

	// Handle deletion
	if !tpuNodeGroup.DeletionTimestamp.IsZero() {
		logger.Info("TPUNodeGroup is being deleted")
		if controllerutil.ContainsFinalizer(&tpuNodeGroup, finalizerName) {
			logger.Info("Cordoning nodes")
			if err := cordonNodes(ctx, logger, r.Client, r.igmClient, &tpuNodeGroup); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to cordon nodes: %w", err)
			}

			logger.Info("Deleting child CRs")
			done, err := deleteChildCRs(ctx, r.Client, &tpuNodeGroup)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete child CRs: %w", err)
			}
			if !done {
				logger.Info("Waiting for child CRs to be deleted")
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}

			// TODO(b/512987019): Implement node object deletion.

			controllerutil.RemoveFinalizer(&tpuNodeGroup, finalizerName)
			if err := r.Update(ctx, &tpuNodeGroup); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if not present
	if controllerutil.AddFinalizer(&tpuNodeGroup, finalizerName) {
		if err := r.Update(ctx, &tpuNodeGroup); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{}, nil // Return and let it reconcile again with finalizer
	}

	// Step 1: Reconcile Workload Policy
	if err := r.reconcileWorkloadPolicy(ctx, &tpuNodeGroup); err != nil {
		var waitErr *WaitingForChildError
		if errors.As(err, &waitErr) {
			logger.Info(waitErr.Error())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to reconcile workload policy: %w", err)
	}

	// Step 2: Reconcile Instance Template
	if err := r.reconcileInstanceTemplate(ctx, &tpuNodeGroup); err != nil {
		var waitErr *WaitingForChildError
		if errors.As(err, &waitErr) {
			// We do not return an error or an explicit requeue timer here.
			// Because the controller is configured with .Owns(&tpuapi.InstanceTemplate{}),
			// any status update to the child CR (e.g., Status.URI being populated)
			// will automatically trigger a new reconciliation request for the parent TPUNodeGroup.
			logger.Info(waitErr.Error())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to reconcile instance template: %w", err)
	}

	// Step 3: Reconcile Managed Instance Group
	if err := r.reconcileManagedInstanceGroup(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile MIG: %w", err)
	}

	// Step 4: Reconcile Node Bootstrapping
	if err := r.reconcileNodeBootstrapping(ctx, logger, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile node bootstrapping: %w", err)
	}

	// Step 4.5: Reconcile Nodes (Labeling and Status)
	if err := ReconcileNodes(ctx, r.Client, r.igmClient, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile nodes: %w", err)
	}

	// Step 5: Reconcile Device Plugin
	if err := deviceplugin.Reconcile(ctx, r.kubeClientset, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile device plugin: %w", err)
	}

	if tpuNodeGroup.Status.NodeSummary != nil && tpuNodeGroup.Status.NodeSummary.Ready < tpuNodeGroup.Spec.NodeCount {
		logger.Info("Nodes are still joining or bootstrapping, requeuing", "ready", tpuNodeGroup.Status.NodeSummary.Ready, "total", tpuNodeGroup.Spec.NodeCount)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileWorkloadPolicy orchestrates the child WorkloadPolicy CR.
// It is only needed for multi-host slices where topology is specified.
func (r *TPUNodeGroupReconciler) reconcileWorkloadPolicy(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// WorkloadPolicy is only needed for multi-host slices where topology is specified.
	if group.Spec.Topology == "" {
		r.Log.Info("Skipping WorkloadPolicy reconciliation as topology is not specified")
		return nil
	}

	// 1. Generate desired state
	policy, err := converter.ToWorkloadPolicyCR(group)
	if err != nil {
		return fmt.Errorf("failed to convert to WorkloadPolicy CR: %w", err)
	}

	// 2. Get existing CR
	existing := &tpuapi.WorkloadPolicy{}
	err = r.Get(ctx, client.ObjectKey{Namespace: policy.Namespace, Name: policy.Name}, existing)
	
	// 3. Create if not found
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Creating WorkloadPolicy CR", "name", policy.Name)
			if err := r.Create(ctx, policy); err != nil {
				return fmt.Errorf("creating WorkloadPolicy CR: %w", err)
			}
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    "WorkloadPolicyReady",
				Status:  metav1.ConditionFalse,
				Reason:  "Provisioning",
				Message: "Child WorkloadPolicy CR created; waiting for GCE resource provisioning",
			})
			return &WaitingForChildError{ChildKind: "WorkloadPolicy"}
		}
		return fmt.Errorf("getting WorkloadPolicy CR: %w", err)
	}

	// 4. Update if changed
	if !equality.Semantic.DeepEqual(existing.Spec, policy.Spec) {
		r.Log.Info("Patching WorkloadPolicy CR", "name", policy.Name)
		patchBase := existing.DeepCopy()
		existing.Spec = policy.Spec
		if err := r.Patch(ctx, existing, client.MergeFrom(patchBase)); err != nil {
			return fmt.Errorf("patching WorkloadPolicy CR: %w", err)
		}
	}

	// 5. Wait for URI population by the WorkloadPolicy controller
	if existing.Status.URI == "" {
		r.Log.Info("WorkloadPolicy CR ready but URI missing", "name", existing.Name)
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    "WorkloadPolicyReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Waiting for GCE resource provisioning",
		})
		return &WaitingForChildError{ChildKind: "WorkloadPolicy"}
	}

	// 6. Mark Ready
	r.Log.Info("WorkloadPolicy CR is ready", "uri", existing.Status.URI)
	meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:    "WorkloadPolicyReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "WorkloadPolicy provisioned successfully",
	})
	return nil
}

// WaitingForChildError indicates that reconciliation is waiting for a child resource to be provisioned.
type WaitingForChildError struct {
	ChildKind string
}

func (e *WaitingForChildError) Error() string {
	return fmt.Sprintf("waiting for child resource %s to be provisioned", e.ChildKind)
}

func (r *TPUNodeGroupReconciler) reconcileInstanceTemplate(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	if group.Spec.InstanceTemplateURI != nil {
		return nil
	}

	template := converter.ToInstanceTemplateCR(group)
	if template == nil {
		return nil
	}

	r.defaultInstanceTemplate(template)

	existing := &tpuapi.InstanceTemplate{}
	err := r.Get(ctx, client.ObjectKey{Namespace: template.Namespace, Name: template.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.Log.Info("Creating InstanceTemplate CR", "name", template.Name)
			if err := r.Create(ctx, template); err != nil {
				return fmt.Errorf("creating InstanceTemplate CR: %w", err)
			}
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    "InstanceTemplateReady",
				Status:  metav1.ConditionFalse,
				Reason:  "Provisioning",
				Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
			})
			return &WaitingForChildError{ChildKind: "InstanceTemplate"}
		}
		return fmt.Errorf("getting InstanceTemplate CR: %w", err)
	}

	if !equality.Semantic.DeepEqual(existing.Spec, template.Spec) {
		r.Log.Info("Patching InstanceTemplate CR", "name", template.Name)
		patchBase := existing.DeepCopy()
		existing.Spec = template.Spec
		if err := r.Patch(ctx, existing, client.MergeFrom(patchBase)); err != nil {
			return fmt.Errorf("patching InstanceTemplate CR: %w", err)
		}
	}

	if existing.Status.URI == "" {
		r.Log.Info("InstanceTemplate CR ready but URI missing", "name", existing.Name)
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    "InstanceTemplateReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Waiting for GCE resource provisioning",
		})
		return &WaitingForChildError{ChildKind: "InstanceTemplate"}
	}

	r.Log.Info("InstanceTemplate CR is ready", "uri", existing.Status.URI)
	meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:    "InstanceTemplateReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "InstanceTemplate provisioned successfully",
	})
	return nil
}

func (r *TPUNodeGroupReconciler) reconcileManagedInstanceGroup(ctx context.Context, group *tpuapi.TPUNodeGroup) error {
	// TODO: Implement MIG reconciliation.
	// Create MIG in bulk mode referencing policy and template.
	return nil
}

func (r *TPUNodeGroupReconciler) reconcileNodeBootstrapping(ctx context.Context, logger logr.Logger, group *tpuapi.TPUNodeGroup) error {
	logger.Info("Bootstrapping TPU nodes", "name", group.Name)
	bootstrapper := NewNodeBootstrapper(r.Client, r.igmClient, r.instanceClient)

	if group.Spec.BootstrapKubernetes != nil {
		// Inject tokens for new instances if needed
		if err := bootstrapper.InjectJoinTokens(ctx, group); err != nil {
			return fmt.Errorf("failed to inject join tokens: %w", err)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TPUNodeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("TPUNodeGroupController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuapi.TPUNodeGroup{}).
		Owns(&tpuapi.InstanceTemplate{}).
		Owns(&tpuapi.WorkloadPolicy{}).
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
