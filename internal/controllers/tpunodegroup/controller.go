package tpunodegroup

import (
	"context"
	"fmt"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/tpunodegroup/deviceplugin"
	"github.com/gke-labs/tpu-operator/internal/converter"
	"github.com/gke-labs/tpu-operator/internal/gce"
	"k8s.io/utils/ptr"
)

const (
	finalizerMIG      = "tpu.google.com/cleanup-mig"
	finalizerTemplate = "tpu.google.com/cleanup-template"
	finalizerPolicy   = "tpu.google.com/cleanup-policy"
	finalizerNodes    = "tpu.google.com/cleanup-nodes"
	finalizerDevicePlugin = "tpu.google.com/cleanup-device-plugin"
)

// TPUNodeGroupReconciler reconciles a TPUNodeGroup object.
type TPUNodeGroupReconciler struct {
	client.Client
	scheme         *runtime.Scheme
	recorder       record.EventRecorder
	igmClient      gce.IGMClient
	instanceClient gce.InstanceClient
	kubeClientset  kubernetes.Interface
}

// NewTPUNodeGroupReconciler creates a new TPUNodeGroupReconciler.
func NewTPUNodeGroupReconciler(client client.Client, scheme *runtime.Scheme, kubeClientset kubernetes.Interface, igmClient gce.IGMClient, instanceClient gce.InstanceClient) *TPUNodeGroupReconciler {
	return &TPUNodeGroupReconciler{
		Client:         client,
		scheme:         scheme,
		kubeClientset:  kubeClientset,
		igmClient:      igmClient,
		instanceClient: instanceClient,
	}
}

// WithRecorder sets the event recorder for the reconciler (useful for testing).
func (r *TPUNodeGroupReconciler) WithRecorder(recorder record.EventRecorder) *TPUNodeGroupReconciler {
	r.recorder = recorder
	return r
}

// +kubebuilder:rbac:groups=tpu.google.com,resources=*,verbs=*
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main entry point for reconciling a TPUNodeGroup.
//
// ARCHITECTURAL NOTICE ON STATUS PATCHING:
// Sub-reconcilers (e.g., reconcileInstanceTemplate, ReconcileNodeJoin) must act
// strictly as in-memory mutators of the TPUNodeGroup struct. They should never issue
// intermediate Status().Patch() calls. Intermediate patches overwrite the in-memory object
// with server state, wiping out unpersisted condition changes. All cumulative state
// updates are persisted atomically by the single Status().Patch() call at the end of Reconcile.
func (r *TPUNodeGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, retErr error) {
	logger := ctrl.LoggerFrom(ctx)

	// 1. Fetch the TPUNodeGroup resource
	var tpuNodeGroup tpuapi.TPUNodeGroup
	if err := r.Get(ctx, req.NamespacedName, &tpuNodeGroup); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("tpuNodeGroup no longer exists")
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
		if retErr != nil {
			r.recorder.Event(&tpuNodeGroup, corev1.EventTypeWarning, "Failed", fmt.Sprintf("Error reconciling: %v", retErr))
			meta.SetStatusCondition(&tpuNodeGroup.Status.Conditions, metav1.Condition{
				Type:    tpuapi.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  tpuapi.ReasonReconcileError,
				Message: fmt.Sprintf("Error reconciling: %v", retErr),
			})
		}
		if err := r.Status().Patch(ctx, &tpuNodeGroup, client.MergeFrom(base)); err != nil {
			if retErr == nil {
				retErr = fmt.Errorf("failed to patch status: %w", err)
			} else {
				logger.Error(err, "failed to patch status after reconcile error")
			}
		}
	}()

	logger.Info("reconciling TPUNodeGroup")

	// Handle deletion
	if !tpuNodeGroup.DeletionTimestamp.IsZero() {
		return handleDeletion(ctx, logger, r.Client, r.recorder, &tpuNodeGroup)
	}

	// Add finalizers if not present
	updated, err := r.ensureFinalizers(ctx, &tpuNodeGroup)
	if err != nil {
		return ctrl.Result{}, err
	}
	if updated {
		return ctrl.Result{}, nil // Return and let it reconcile again with finalizers
	}

	// Step 1: Reconcile Workload Policy
	if ready, err := r.reconcileWorkloadPolicy(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile workload policy: %w", err)
	} else if !ready {
		return ctrl.Result{}, nil
	}

	// Step 2: Reconcile Instance Template
	if ready, err := r.reconcileInstanceTemplate(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile instance template: %w", err)
	} else if !ready {
		return ctrl.Result{}, nil
	}

	// Step 3: Reconcile Managed Instance Group
	if ready, err := r.reconcileManagedInstanceGroup(ctx, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile MIG: %w", err)
	} else if !ready {
		return ctrl.Result{}, nil
	}

	// Step 4: Inject Metadata
	if err := injectMetadata(ctx, &tpuNodeGroup, r.Client, r.igmClient, r.instanceClient); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to inject metadata: %w", err)
	}

	// Step 4.5: Reconcile Nodes (Labeling and Status)
	if err := ReconcileNodes(ctx, r.Client, r.igmClient, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile nodes: %w", err)
	}

	var prevReady int32
	if base.Status.NodeSummary != nil {
		prevReady = base.Status.NodeSummary.Ready
	}
	var currentReady int32
	if tpuNodeGroup.Status.NodeSummary != nil {
		currentReady = tpuNodeGroup.Status.NodeSummary.Ready
	}
	if currentReady > prevReady && currentReady < tpuNodeGroup.Spec.NodeCount {
		r.recorder.Event(&tpuNodeGroup, corev1.EventTypeNormal, "NodesJoining", fmt.Sprintf("Waiting for %d of %d nodes to join the cluster", tpuNodeGroup.Spec.NodeCount-currentReady, tpuNodeGroup.Spec.NodeCount))
	}

	// Step 5: Reconcile Device Plugin
	if err := deviceplugin.Reconcile(ctx, r.kubeClientset, &tpuNodeGroup); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile device plugin: %w", err)
	}

	if tpuNodeGroup.Status.NodeSummary != nil && tpuNodeGroup.Status.NodeSummary.Ready < tpuNodeGroup.Spec.NodeCount {
		logger.V(1).Info("nodes are still joining or bootstrapping, requeuing", "ready", tpuNodeGroup.Status.NodeSummary.Ready, "total", tpuNodeGroup.Spec.NodeCount)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// All steps successful
	meta.SetStatusCondition(&tpuNodeGroup.Status.Conditions, metav1.Condition{
		Type:    tpuapi.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  tpuapi.ReasonReady,
		Message: "All nodes are ready",
	})

	if !meta.IsStatusConditionTrue(base.Status.Conditions, tpuapi.ConditionTypeReady) {
		r.recorder.Event(&tpuNodeGroup, corev1.EventTypeNormal, "Provisioned", "All nodes are ready")
	}

	return ctrl.Result{}, nil
}

// ensureFinalizers ensures that the required finalizers are present on the TPUNodeGroup.
// It returns true if the object was updated, and false otherwise.
func (r *TPUNodeGroupReconciler) ensureFinalizers(ctx context.Context, group *tpuapi.TPUNodeGroup) (bool, error) {
	patchBase := group.DeepCopy()
	updated := false
	if controllerutil.AddFinalizer(group, finalizerMIG) {
		updated = true
	}
	if controllerutil.AddFinalizer(group, finalizerTemplate) {
		updated = true
	}
	if controllerutil.AddFinalizer(group, finalizerPolicy) {
		updated = true
	}
	if controllerutil.AddFinalizer(group, finalizerNodes) {
		updated = true
	}
	if controllerutil.AddFinalizer(group, finalizerDevicePlugin) {
		updated = true
	}

	if !updated {
		return false, nil
	}

	if err := r.Patch(ctx, group, client.MergeFrom(patchBase)); err != nil {
		return false, fmt.Errorf("failed to patch finalizers: %w", err)
	}
	return true, nil
}

// reconcileWorkloadPolicy orchestrates the child WorkloadPolicy CR.
// It is only needed for multi-host slices where topology is specified.
func (r *TPUNodeGroupReconciler) reconcileWorkloadPolicy(ctx context.Context, group *tpuapi.TPUNodeGroup) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	// WorkloadPolicy is only needed for multi-host slices where topology is specified.
	if group.Spec.Topology == "" || group.Spec.TargetSizePolicyMode == tpuapi.TargetSizePolicyModeIndividual {
		logger.V(1).Info("skipping WorkloadPolicy reconciliation as topology is not specified or target policy mode is INDIVIDUAL")
		return true, nil
	}

	// 1. Generate desired state
	policy, err := converter.ToWorkloadPolicyCR(group)
	if err != nil {
		return false, fmt.Errorf("failed to convert to WorkloadPolicy CR: %w", err)
	}

	// 2. Get existing CR
	existing := &tpuapi.WorkloadPolicy{}
	err = r.Get(ctx, client.ObjectKey{Namespace: policy.Namespace, Name: policy.Name}, existing)

	// 3. Create if not found
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("creating WorkloadPolicy CR", "name", policy.Name)
			if err := r.Create(ctx, policy); err != nil {
				return false, fmt.Errorf("creating WorkloadPolicy CR: %w", err)
			}
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    "WorkloadPolicyReady",
				Status:  metav1.ConditionFalse,
				Reason:  "Provisioning",
				Message: "Child WorkloadPolicy CR created; waiting for GCE resource provisioning",
			})
			return false, nil
		}
		return false, fmt.Errorf("getting WorkloadPolicy CR: %w", err)
	}

	// 4. Update if changed
	if !equality.Semantic.DeepEqual(existing.Spec, policy.Spec) {
		logger.Info("patching WorkloadPolicy CR", "name", policy.Name)
		patchBase := existing.DeepCopy()
		existing.Spec = policy.Spec
		if err := r.Patch(ctx, existing, client.MergeFrom(patchBase)); err != nil {
			return false, fmt.Errorf("patching WorkloadPolicy CR: %w", err)
		}
	}

	// 5. Wait for URI population by the WorkloadPolicy controller
	if existing.Status.URI == "" {
		logger.V(1).Info("workloadPolicy CR ready but URI missing", "name", existing.Name)
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    "WorkloadPolicyReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Waiting for GCE resource provisioning",
		})
		return false, nil
	}

	// 6. Mark Ready
	logger.Info("workloadPolicy CR is ready", "uri", existing.Status.URI)
	meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:    "WorkloadPolicyReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "WorkloadPolicy provisioned successfully",
	})
	return true, nil
}

func (r *TPUNodeGroupReconciler) reconcileInstanceTemplate(ctx context.Context, group *tpuapi.TPUNodeGroup) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)

	template := converter.ToInstanceTemplateCR(group)
	if template == nil {
		return true, nil
	}

	if err := r.defaultInstanceTemplate(template, group); err != nil {
		return false, err
	}

	existing := &tpuapi.InstanceTemplate{}
	err := r.Get(ctx, client.ObjectKey{Namespace: template.Namespace, Name: template.Name}, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("creating InstanceTemplate CR", "name", template.Name)
			if err := r.Create(ctx, template); err != nil {
				return false, fmt.Errorf("creating InstanceTemplate CR: %w", err)
			}
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    "InstanceTemplateReady",
				Status:  metav1.ConditionFalse,
				Reason:  "Provisioning",
				Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
			})
			return false, nil
		}
		return false, fmt.Errorf("getting InstanceTemplate CR: %w", err)
	}

	if !equality.Semantic.DeepEqual(existing.Spec, template.Spec) {
		logger.Info("patching InstanceTemplate CR", "name", template.Name)
		patchBase := existing.DeepCopy()
		existing.Spec = template.Spec
		if err := r.Patch(ctx, existing, client.MergeFrom(patchBase)); err != nil {
			return false, fmt.Errorf("patching InstanceTemplate CR: %w", err)
		}
	}

	if existing.Status.URI == "" {
		logger.V(1).Info("instanceTemplate CR ready but URI missing", "name", existing.Name)
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    "InstanceTemplateReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Waiting for GCE resource provisioning",
		})
		return false, nil
	}

	logger.Info("instanceTemplate CR is ready", "uri", existing.Status.URI)
	meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:    "InstanceTemplateReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "InstanceTemplate provisioned successfully",
	})
	return true, nil
}

func (r *TPUNodeGroupReconciler) reconcileManagedInstanceGroup(ctx context.Context, group *tpuapi.TPUNodeGroup) (bool, error) {
	logger := ctrl.LoggerFrom(ctx)
	var template *tpuapi.InstanceTemplate
	var policy *tpuapi.WorkloadPolicy
	var err error

	// 1. Fetch InstanceTemplate if needed
	template = &tpuapi.InstanceTemplate{}
	templateName := group.InstanceTemplateName()
	err = r.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: templateName}, template)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting InstanceTemplate CR: %w", err)
	}
	if template.Status.URI == "" {
		return false, nil
	}

	// 2. Fetch WorkloadPolicy if needed
	if group.Spec.Topology != "" && group.Spec.TargetSizePolicyMode == tpuapi.TargetSizePolicyModeBulk {
		policy = &tpuapi.WorkloadPolicy{}
		policyName := group.WorkloadPolicyName()
		err = r.Get(ctx, client.ObjectKey{Namespace: group.Namespace, Name: policyName}, policy)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, fmt.Errorf("getting WorkloadPolicy CR: %w", err)
		}
		if policy.Status.URI == "" {
			return false, nil
		}
	}

	// 3. Generate desired state
	mig := converter.ToManagedInstanceGroupCR(group, template, policy)

	// 4. Get existing CR
	existing := &tpuapi.ManagedInstanceGroup{}
	err = r.Get(ctx, client.ObjectKey{Namespace: mig.Namespace, Name: mig.Name}, existing)

	// 5. Create if not found
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("creating ManagedInstanceGroup CR", "name", mig.Name)
			if err := r.Create(ctx, mig); err != nil {
				return false, fmt.Errorf("creating ManagedInstanceGroup CR: %w", err)
			}
			meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
				Type:    "ManagedInstanceGroupReady",
				Status:  metav1.ConditionFalse,
				Reason:  "Provisioning",
				Message: "Child ManagedInstanceGroup CR created; waiting for GCE resource provisioning",
			})
			return false, nil
		}
		return false, fmt.Errorf("getting ManagedInstanceGroup CR: %w", err)
	}

	// 6. Update if changed
	if !equality.Semantic.DeepEqual(existing.Spec, mig.Spec) {
		logger.Info("patching ManagedInstanceGroup CR", "name", mig.Name)
		patchBase := existing.DeepCopy()
		existing.Spec = mig.Spec
		if err := r.Patch(ctx, existing, client.MergeFrom(patchBase)); err != nil {
			return false, fmt.Errorf("patching ManagedInstanceGroup CR: %w", err)
		}
	}

	// 7. Wait for URL population
	if existing.Status.URL == "" {
		logger.V(1).Info("managedInstanceGroup CR ready but URL missing", "name", existing.Name)
		meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
			Type:    "ManagedInstanceGroupReady",
			Status:  metav1.ConditionFalse,
			Reason:  "Provisioning",
			Message: "Waiting for GCE resource provisioning",
		})
		return false, nil
	}

	// 8. Mark Ready
	logger.Info("managedInstanceGroup CR is ready", "url", existing.Status.URL)
	wasReady := meta.IsStatusConditionTrue(group.Status.Conditions, "ManagedInstanceGroupReady")
	meta.SetStatusCondition(&group.Status.Conditions, metav1.Condition{
		Type:    "ManagedInstanceGroupReady",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "ManagedInstanceGroup provisioned successfully",
	})
	if !wasReady {
		r.recorder.Event(group, corev1.EventTypeNormal, "ChildResourcesProvisioned", "All child resources provisioned successfully")
	}
	return true, nil
}

// mapDaemonSetToTPUNodeGroups maps events on the shared TPU device plugin DaemonSet
// to reconciliation requests for all active TPUNodeGroups.
func (r *TPUNodeGroupReconciler) mapDaemonSetToTPUNodeGroups(ctx context.Context, obj client.Object) []reconcile.Request {
	ds, ok := obj.(*appsv1.DaemonSet)
	if !ok {
		return nil
	}
	if ds.Name != deviceplugin.DevicePluginName || ds.Namespace != deviceplugin.DevicePluginNamespace {
		return nil
	}

	// List all TPUNodeGroups cluster-wide
	var list tpuapi.TPUNodeGroupList
	if err := r.List(ctx, &list); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "failed to list TPUNodeGroups in DaemonSet watch mapper")
		return nil
	}

	var requests []reconcile.Request
	for _, tg := range list.Items {
		// Only reconcile active groups to heal the missing DaemonSet
		if tg.DeletionTimestamp.IsZero() {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&tg),
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *TPUNodeGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.recorder = mgr.GetEventRecorderFor("TPUNodeGroupController")
	return ctrl.NewControllerManagedBy(mgr).
		For(&tpuapi.TPUNodeGroup{}).
		Owns(&tpuapi.InstanceTemplate{}).
		Owns(&tpuapi.WorkloadPolicy{}).
		Owns(&tpuapi.ManagedInstanceGroup{}).
		Watches(
			&appsv1.DaemonSet{},
			handler.EnqueueRequestsFromMapFunc(r.mapDaemonSetToTPUNodeGroups),
		).
		Complete(r)
}

// defaultInstanceTemplate populates default values for an InstanceTemplate prior to reconciliation.
func (r *TPUNodeGroupReconciler) defaultInstanceTemplate(template *tpuapi.InstanceTemplate, group *tpuapi.TPUNodeGroup) error {
	if template == nil {
		return nil
	}
	if template.Spec.InstanceConfig.ProvisioningModel == nil {
		if template.Spec.InstanceConfig.Reservation != nil {
			template.Spec.InstanceConfig.ProvisioningModel = ptr.To("RESERVATION_BOUND")
		} else {
			template.Spec.InstanceConfig.ProvisioningModel = ptr.To("STANDARD")
		}
	}

	// Include the startup script only if BootstrapKubernetes is specified.
	if group.Spec.BootstrapKubernetes != nil {
		if template.Spec.Metadata == nil {
			template.Spec.Metadata = make(map[string]string)
		}
		if group.Spec.BootstrapKubernetes.Version == nil {
			return fmt.Errorf("version must be specified when bootstrapKubernetes is enabled")
		}
		version := *group.Spec.BootstrapKubernetes.Version
		script := renderStartupScript(version, group.Spec.Project, group.Spec.NodeLocation)
		template.Spec.Metadata["startup-script"] = script
	}
	return nil
}



