package tpunodegroup

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/tpunodegroup/deviceplugin"
	"github.com/gke-labs/tpu-operator/internal/gce"
	"k8s.io/utils/ptr"
)

func TestTPUNodeGroupReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding AppsV1 to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	tests := []struct {
		name              string
		request           reconcile.Request
		initialObject     *tpuapi.TPUNodeGroup
		additionalObjects []client.Object
		nodes             []corev1.Node
		wantResult        reconcile.Result
		wantErr           bool
		wantDaemonSet     bool
		wantStatus        *tpuapi.NodeSummary
		wantConditions    []metav1.Condition
		wantNodeTaints    map[string][]corev1.Taint
		wantFinalizers    []string
		wantDeleted       bool
		wantNodesDeleted  []string
		wantEvents        []string
		setupMocks        func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient)
		kubeObjects       []runtime.Object
	}{
		{
			name: "resource_not_found",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent",
					Namespace: "default",
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
		{
			name: "add_finalizers",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantFinalizers: []string{
				"tpu.google.com/cleanup-mig",
				"tpu.google.com/cleanup-template",
				"tpu.google.com/cleanup-policy",
				"tpu.google.com/cleanup-nodes",
				"tpu.google.com/cleanup-device-plugin",
			},
		},
		{
			name: "resource_found_success",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
					BootstrapKubernetes: &tpuapi.BootstrapConfig{
						Version:        ptr.To("1.25.0"),
						ControlPlaneIP: "1.2.3.4",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           1,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			wantResult:    reconcile.Result{RequeueAfter: 30 * time.Second},
			wantErr:       false,
			wantDaemonSet: true,
			wantStatus: &tpuapi.NodeSummary{
				Ready:       0,
				Reconciling: 1,
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
							InstanceStatus: ptr.To("RUNNING"),
						},
					}, nil
				}
				inst.GetFunc = func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
					return &computepb.Instance{
						Name: ptr.To("test-tpu-0"),
						Metadata: &computepb.Metadata{
							Fingerprint: ptr.To("fingerprint"),
							Items: []*computepb.Items{
								{Key: ptr.To("existing-key"), Value: ptr.To("existing-value")},
							},
						},
					}, nil
				}
				inst.SetMetadataFunc = func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
					return &compute.Operation{}, nil
				}
			},
		},
		{
			name: "resource_found_bootstrapping_disabled",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu-disabled",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu-disabled",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-disabled-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-disabled-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           1,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-disabled-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu-disabled",
						},
					},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/test-tpu-0",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			wantResult:    reconcile.Result{},
			wantErr:       false,
			wantDaemonSet: true,
			wantStatus: &tpuapi.NodeSummary{
				Ready:       1,
				Reconciling: 0,
			},
			kubeObjects: []runtime.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tpu-device-plugin",
						Namespace: "kube-system",
					},
					Status: appsv1.DaemonSetStatus{
						NumberReady:            1,
						DesiredNumberScheduled: 1,
					},
				},
			},
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "ManagedInstanceGroup provisioned successfully",
				},
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionTrue,
					Reason:  tpuapi.ReasonReady,
					Message: "All nodes are ready",
				},
			},
			wantEvents: []string{
				"Normal ChildResourcesProvisioned All child resources provisioned successfully",
				"Normal Provisioned All nodes are ready",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
							InstanceStatus: ptr.To("RUNNING"),
						},
					}, nil
				}
				inst.GetFunc = func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
					return &computepb.Instance{
						Name: ptr.To("test-tpu-0"),
						Metadata: &computepb.Metadata{
							Fingerprint: ptr.To("fingerprint"),
						},
					}, nil
				}
				inst.SetMetadataFunc = func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
					return &compute.Operation{}, nil
				}
			},
		},

		{
			name: "reconcile_instance_template_create",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
				},
			},
		},
		{
			name: "reconcile_instance_template_ready",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           1,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			wantResult:    reconcile.Result{RequeueAfter: 30 * time.Second},
			wantErr:       false,
			wantDaemonSet: true,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "ManagedInstanceGroup provisioned successfully",
				},
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonAwaitingNodeJoin,
					Message: "Waiting for 1 of 1 nodes to join the cluster",
				},
			},
		},
		{
			name: "reconcile_workload_policy_create",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            2,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "BULK",
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "WorkloadPolicyReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child WorkloadPolicy CR created; waiting for GCE resource provisioning",
				},
			},
		},
		{
			name: "reconcile_workload_policy_ready",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            2,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "BULK",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.WorkloadPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-policy",
						Namespace: "default",
					},
					Spec: tpuapi.WorkloadPolicySpec{
						Project:             "test-project",
						Region:              "us-central1",
						AcceleratorTopology: "2x2x1",
					},
					Status: tpuapi.WorkloadPolicyStatus{
						URI: "projects/test-project/regions/us-central1/resourcePolicies/test-tpu-policy",
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "WorkloadPolicyReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "WorkloadPolicy provisioned successfully",
				},
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
				},
			},
		},

		{
			name: "reconcile_workload_policy_skip_single_host_with_topology",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
				},
			},
		},

		{
			name: "reconcile_workload_policy_skip",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child InstanceTemplate CR created; waiting for GCE resource provisioning",
				},
			},
		},
		{
			name: "resource_deletion_cordon",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantNodeTaints: map[string][]corev1.Taint{
				"test-tpu-0": {
					{
						Key:    corev1.TaintNodeUnschedulable,
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			wantEvents: []string{
				"Normal Cleanup TPU Node Group deletion initiated",
			},
		},
		{
			name: "resource_deletion_policy_gone_wait_nodes",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-nodes"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
			},
			wantResult:       reconcile.Result{},
			wantErr:          false,
			wantNodesDeleted: []string{"test-tpu-0"},
			wantDeleted:      true,
		},
		{
			name: "resource_deletion_wait_mig",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
			},
			wantNodeTaints: map[string][]corev1.Taint{
				"test-tpu-0": {
					{
						Key:    corev1.TaintNodeUnschedulable,
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonDeletingMIG,
					Message: "Waiting for ManagedInstanceGroup to be deleted",
				},
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
						},
					}, nil
				}
			},
		},
		{
			name: "resource_deletion_wait_mig_deleting",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-tpu-mig",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonDeletingMIG,
					Message: "Waiting for ManagedInstanceGroup to be deleted",
				},
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{}, nil
				}
			},
		},
		{
			name: "resource_deletion_mig_gone_wait_template",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonDeletingTemplate,
					Message: "Waiting for InstanceTemplate to be deleted",
				},
			},
		},
		{
			name: "resource_deletion_template_gone_wait_policy",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-policy"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.WorkloadPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-policy",
						Namespace: "default",
					},
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonDeletingPolicy,
					Message: "Waiting for WorkloadPolicy to be deleted",
				},
			},
		},
		{
			name: "resource_deletion_all_gone",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-policy"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			wantResult:  reconcile.Result{},
			wantErr:     false,
			wantDeleted: true,
		},
		{
			name: "resource_deletion_nodes_gone_wait_device_plugin",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-device-plugin"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tpu-device-plugin",
						Namespace: "kube-system",
					},
				},
			},
			wantResult:  reconcile.Result{},
			wantErr:     false,
			wantDeleted: true,
		},
		{
			name: "resource_deletion_nodes_gone_wait_device_plugin_with_dummy_finalizer",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-tpu",
					Namespace:         "default",
					Finalizers:        []string{"tpu.google.com/cleanup-device-plugin", "tpu.google.com/dummy-cleanup"},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
				},
			},
			additionalObjects: []client.Object{
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tpu-device-plugin",
						Namespace: "kube-system",
					},
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/dummy-cleanup"},
			wantConditions: []metav1.Condition{
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonDeletingDevicePlugin,
					Message: "Deleting TPU Device Plugin DaemonSet",
				},
			},
		},
		{
			name: "reconcile_instance_template_external_success",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceTemplateURI:  ptr.To("projects/test-project/locations/global/instanceTemplates/my-template"),
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "ExternalTemplate",
					Message: "Using external instance template",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child ManagedInstanceGroup CR created; waiting for GCE resource provisioning",
				},
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("tpu7x-standard-4t"),
							Scheduling: &computepb.Scheduling{
								ProvisioningModel: ptr.To("STANDARD"),
								OnHostMaintenance: ptr.To("TERMINATE"),
							},
						},
					}, nil
				}
			},
		},
		{
			name: "reconcile_instance_template_external_invalid_machine_type",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceTemplateURI:  ptr.To("projects/test-project/locations/global/instanceTemplates/my-template"),
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidTemplate",
					Message: "invalid external template: machine type \"n1-standard-1\" is not supported",
				},
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "ReconcileError",
					Message: "Error reconciling: failed to reconcile instance template: validating external template: invalid external template: machine type \"n1-standard-1\" is not supported",
				},
			},
			wantEvents: []string{
				"Warning InvalidInstanceTemplate External instance template validation failed: invalid external template: machine type \"n1-standard-1\" is not supported",
				"Warning Failed Error reconciling: failed to reconcile instance template: validating external template: invalid external template: machine type \"n1-standard-1\" is not supported",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("n1-standard-1"),
						},
					}, nil
				}
			},
		},
		{
			name: "reconcile_instance_template_external_invalid_machine_type_subsequent",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceTemplateURI:  ptr.To("projects/test-project/locations/global/instanceTemplates/my-template"),
				},
				Status: tpuapi.TPUNodeGroupStatus{
					Conditions: []metav1.Condition{
						{
							Type:    "InstanceTemplateReady",
							Status:  metav1.ConditionFalse,
							Reason:  "InvalidTemplate",
							Message: "invalid external template: machine type \"n1-standard-1\" is not supported",
						},
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionFalse,
					Reason:  "InvalidTemplate",
					Message: "invalid external template: machine type \"n1-standard-1\" is not supported",
				},
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "ReconcileError",
					Message: "Error reconciling: failed to reconcile instance template: validating external template: invalid external template: machine type \"n1-standard-1\" is not supported",
				},
			},
			wantEvents: []string{
				"Warning Failed Error reconciling: failed to reconcile instance template: validating external template: invalid external template: machine type \"n1-standard-1\" is not supported",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				tmpl.GetFunc = func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return &computepb.InstanceTemplate{
						Properties: &computepb.InstanceProperties{
							MachineType: ptr.To("n1-standard-1"),
						},
					}, nil
				}
			},
		},
		{
			name: "reconcile_mig_create",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionFalse,
					Reason:  "Provisioning",
					Message: "Child ManagedInstanceGroup CR created; waiting for GCE resource provisioning",
				},
			},
		},
		{
			name: "reconcile_nodes_joining",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            2,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
				Status: tpuapi.TPUNodeGroupStatus{
					NodeSummary: &tpuapi.NodeSummary{
						Ready: 0,
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           2,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/test-tpu-0",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			wantResult:    reconcile.Result{RequeueAfter: 30 * time.Second},
			wantErr:       false,
			wantDaemonSet: true,
			wantStatus: &tpuapi.NodeSummary{
				Ready:       1,
				Reconciling: 1,
			},
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "ManagedInstanceGroup provisioned successfully",
				},
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonAwaitingNodeJoin,
					Message: "Waiting for 1 of 2 nodes to join the cluster",
				},
			},
			wantEvents: []string{
				"Normal ChildResourcesProvisioned All child resources provisioned successfully",
				"Normal NodesJoining Waiting for 1 of 2 nodes to join the cluster",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
							InstanceStatus: ptr.To("RUNNING"),
						},
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-1"),
							InstanceStatus: ptr.To("PROVISIONING"),
						},
					}, nil
				}
				inst.GetFunc = func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
					return &computepb.Instance{
						Name: ptr.To(req.Instance),
						Metadata: &computepb.Metadata{
							Fingerprint: ptr.To("fingerprint"),
						},
					}, nil
				}
				inst.SetMetadataFunc = func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
					return &compute.Operation{}, nil
				}
			},
		},
		{
			name: "reconcile_failed_event",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           1,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    true,
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "ManagedInstanceGroup provisioned successfully",
				},
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonReconcileError,
					Message: "Error reconciling: failed to inject metadata: failed to list managed instances: GCE API error",
				},
			},
			wantEvents: []string{
				"Normal ChildResourcesProvisioned All child resources provisioned successfully",
				"Warning Failed Error reconciling: failed to inject metadata: failed to list managed instances: GCE API error",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return nil, fmt.Errorf("GCE API error")
				}
			},
		},
		{
			name: "reconcile_mig_provisioned",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObject: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-tpu",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes", "tpu.google.com/cleanup-device-plugin"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:              "test-project",
					NodeLocation:         "us-central1-a",
					NodeCount:            1,
					Topology:             "2x2x1",
					TargetSizePolicyMode: "INDIVIDUAL",
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
					},
				},
				Status: tpuapi.TPUNodeGroupStatus{
					Conditions: []metav1.Condition{
						{
							Type:    "InstanceTemplateReady",
							Status:  metav1.ConditionTrue,
							Reason:  "Ready",
							Message: "InstanceTemplate provisioned successfully",
						},
						{
							Type:    "ManagedInstanceGroupReady",
							Status:  metav1.ConditionFalse,
							Reason:  "Provisioning",
							Message: "Child ManagedInstanceGroup CR created; waiting for GCE resource provisioning",
						},
					},
				},
			},
			additionalObjects: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-template",
						Namespace: "default",
					},
					Spec: tpuapi.InstanceTemplateSpec{
						InstanceConfig: tpuapi.InstanceConfig{
							MachineType:       "tpu7x-standard-4t",
							ProvisioningModel: ptr.To("STANDARD"),
						},
					},
					Status: tpuapi.InstanceTemplateStatus{
						URI: "projects/test-project/global/instanceTemplates/my-template",
					},
				},
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-tpu-mig",
						Namespace: "default",
					},
					Spec: tpuapi.ManagedInstanceGroupSpec{
						Project:              "test-project",
						Location:             "us-central1-a",
						InstanceTemplate:     "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:           1,
						TargetSizePolicyMode: "INDIVIDUAL",
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
						Conditions: []metav1.Condition{
							{
								Type:    tpuapi.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  tpuapi.ReasonReady,
								Message: "ManagedInstanceGroup provisioned successfully",
							},
						},
					},
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 30 * time.Second},
			wantErr:    false,
			wantStatus: &tpuapi.NodeSummary{
				Ready:       0,
				Reconciling: 1,
			},
			wantConditions: []metav1.Condition{
				{
					Type:    "InstanceTemplateReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "InstanceTemplate provisioned successfully",
				},
				{
					Type:    "ManagedInstanceGroupReady",
					Status:  metav1.ConditionTrue,
					Reason:  "Ready",
					Message: "ManagedInstanceGroup provisioned successfully",
				},
				{
					Type:    tpuapi.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  tpuapi.ReasonAwaitingNodeJoin,
					Message: "Waiting for 1 of 1 nodes to join the cluster",
				},
			},
			wantEvents: []string{
				"Normal ChildResourcesProvisioned All child resources provisioned successfully",
			},
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient, tmpl *gce.MockInstanceTemplateClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
							InstanceStatus: ptr.To("RUNNING"),
						},
					}, nil
				}
				inst.GetFunc = func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
					return &computepb.Instance{
						Name: ptr.To("test-tpu-0"),
						Metadata: &computepb.Metadata{
							Fingerprint: ptr.To("fingerprint"),
						},
					}, nil
				}
				inst.SetMetadataFunc = func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
					return &compute.Operation{}, nil
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			caCertPem := generateTestCACert(t)
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kube-root-ca.crt",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"ca.crt": caCertPem,
				},
			}
			objs := []client.Object{cm}
			if tc.initialObject != nil {
				objs = append(objs, tc.initialObject)
			}
			objs = append(objs, tc.additionalObjects...)
			for i := range tc.nodes {
				isReady := false
				for _, cond := range tc.nodes[i].Status.Conditions {
					if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
						isReady = true
						break
					}
				}
				if isReady {
					tc.nodes[i] = withTPU(tc.nodes[i], "4", "4")
				}
				objs = append(objs, &tc.nodes[i])
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
			if tc.initialObject != nil {
				builder = builder.WithStatusSubresource(tc.initialObject)
			}
			cl := builder.Build()
			k8sFakeClient := k8sfake.NewSimpleClientset(tc.kubeObjects...)

			igm := &gce.MockIGMClient{}
			inst := &gce.MockInstanceClient{}
			templateClient := &gce.MockInstanceTemplateClient{}
			if tc.setupMocks != nil {
				tc.setupMocks(igm, inst, templateClient)
			}

			fakeRecorder := record.NewFakeRecorder(10)
			r := NewTPUNodeGroupReconciler(cl, scheme, k8sFakeClient, igm, inst, templateClient).
				WithRecorder(fakeRecorder)

			ctx := logr.NewContext(t.Context(), logr.Discard())
			gotResult, err := r.Reconcile(ctx, tc.request)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("Reconcile(%v) = (%v, %v), want error presence = %v", tc.request, gotResult, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
				t.Errorf("Reconcile(%v) result mismatch (-want +got):\n%s", tc.request, diff)
			}

			if tc.wantDaemonSet {
				// Verify DaemonSet creation in the fake kubeclientset
				_, err := k8sFakeClient.AppsV1().DaemonSets("kube-system").Get(t.Context(), "tpu-device-plugin", metav1.GetOptions{})
				if err != nil {
					if errors.IsNotFound(err) {
						t.Errorf("Expected DaemonSet 'tpu-device-plugin' to be created, but it was not found")
					} else {
						t.Errorf("Failed to get DaemonSet: %v", err)
					}
				}
			}

			if tc.wantDeleted {
				var updatedObject tpuapi.TPUNodeGroup
				err := cl.Get(t.Context(), tc.request.NamespacedName, &updatedObject)
				if err == nil {
					t.Errorf("Expected object to be deleted, but it was found")
				} else if !errors.IsNotFound(err) {
					t.Errorf("Expected NotFound error, got: %v", err)
				}
			} else if tc.wantStatus != nil || tc.wantConditions != nil || tc.wantFinalizers != nil {
				var updatedObject tpuapi.TPUNodeGroup
				if err := cl.Get(t.Context(), tc.request.NamespacedName, &updatedObject); err != nil {
					t.Fatalf("Failed to get updated object: %v", err)
				}

				if tc.wantStatus != nil {
					if diff := cmp.Diff(tc.wantStatus, updatedObject.Status.NodeSummary); diff != "" {
						t.Errorf("Status.NodeSummary mismatch (-want +got):\n%s", diff)
					}
				}

				if tc.wantConditions != nil {
					if diff := cmp.Diff(tc.wantConditions, updatedObject.Status.Conditions, cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime")); diff != "" {
						t.Errorf("Status.Conditions mismatch (-want +got):\n%s", diff)
					}
				}

				if tc.wantFinalizers != nil {
					if diff := cmp.Diff(tc.wantFinalizers, updatedObject.Finalizers); diff != "" {
						t.Errorf("Finalizers mismatch (-want +got):\n%s", diff)
					}
				}
			}

			if tc.wantNodeTaints != nil {
				for nodeName, wantTaints := range tc.wantNodeTaints {
					var node corev1.Node
					if err := cl.Get(t.Context(), types.NamespacedName{Name: nodeName}, &node); err != nil {
						t.Errorf("Failed to get node %s: %v", nodeName, err)
						continue
					}
					if diff := cmp.Diff(wantTaints, node.Spec.Taints); diff != "" {
						t.Errorf("Node %s taints mismatch (-want +got):\n%s", nodeName, diff)
					}
				}
			}

			if tc.wantNodesDeleted != nil {
				for _, nodeName := range tc.wantNodesDeleted {
					var node corev1.Node
					err := cl.Get(t.Context(), types.NamespacedName{Name: nodeName}, &node)
					if err == nil {
						t.Errorf("Expected node %s to be deleted, but it still exists", nodeName)
					} else if !errors.IsNotFound(err) {
						t.Errorf("Failed to get node %s: %v", nodeName, err)
					}
				}
			}

			if tc.wantEvents != nil {
				var gotEvents []string
				for {
					select {
					case ev := <-fakeRecorder.Events:
						gotEvents = append(gotEvents, ev)
					default:
						goto DoneEvents
					}
				}
			DoneEvents:
				if diff := cmp.Diff(tc.wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("Reconcile() events mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestTPUNodeGroupReconciler_defaultInstanceTemplate(t *testing.T) {
	expectedScript := RenderStartupScript("1.31", "test-project", "us-central1-a")

	tests := []struct {
		name     string
		template *tpuapi.InstanceTemplate
		group    *tpuapi.TPUNodeGroup
		want     *tpuapi.InstanceTemplate
	}{
		{
			name:     "nil template",
			template: nil,
			want:     nil,
		},
		{
			name: "empty InstanceConfig defaults to STANDARD and default subnetwork",
			template: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
						Subnetwork:  ptr.To("default"),
					},
				},
			},
			want: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType:       "tpu7x-standard-4t",
						Subnetwork:        ptr.To("default"),
						ProvisioningModel: ptr.To("STANDARD"),
					},
				},
			},
		},
		{
			name: "Reservation present defaults to RESERVATION_BOUND",
			template: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
						Reservation: ptr.To("my-res"),
						Subnetwork:  ptr.To("default"),
					},
				},
			},
			want: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType:       "tpu7x-standard-4t",
						Reservation:       ptr.To("my-res"),
						Subnetwork:        ptr.To("default"),
						ProvisioningModel: ptr.To("RESERVATION_BOUND"),
					},
				},
			},
		},
		{
			name: "fields already set are preserved",
			template: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType:       "tpu7x-standard-4t",
						Reservation:       ptr.To("my-res"),
						Subnetwork:        ptr.To("custom-subnet"),
						ProvisioningModel: ptr.To("SPOT"),
					},
				},
			},
			want: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType:       "tpu7x-standard-4t",
						Reservation:       ptr.To("my-res"),
						Subnetwork:        ptr.To("custom-subnet"),
						ProvisioningModel: ptr.To("SPOT"),
					},
				},
			},
		},
		{
			name: "with BootstrapKubernetes adds startup script",
			template: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType: "tpu7x-standard-4t",
						Subnetwork:  ptr.To("default"),
					},
				},
			},
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					BootstrapKubernetes: &tpuapi.BootstrapConfig{
						Version: ptr.To("1.31"),
					},
				},
			},
			want: &tpuapi.InstanceTemplate{
				Spec: tpuapi.InstanceTemplateSpec{
					InstanceConfig: tpuapi.InstanceConfig{
						MachineType:       "tpu7x-standard-4t",
						Subnetwork:        ptr.To("default"),
						ProvisioningModel: ptr.To("STANDARD"),
						Metadata: map[string]string{
							"startup-script": expectedScript,
						},
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TPUNodeGroupReconciler{}
			group := tc.group
			if group == nil {
				group = &tpuapi.TPUNodeGroup{}
			}
			err := r.defaultInstanceTemplate(tc.template, group)
			if err != nil {
				t.Fatalf("defaultInstanceTemplate() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tc.want, tc.template); diff != "" {
				t.Errorf("defaultInstanceTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestMapDaemonSetToTPUNodeGroups(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Apps API to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Core API to scheme: %v", err)
	}

	tests := []struct {
		name        string
		obj         client.Object
		initialObjs []client.Object
		want        []reconcile.Request
	}{
		{
			name: "valid_ds_active_groups",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deviceplugin.DevicePluginName,
					Namespace: deviceplugin.DevicePluginNamespace,
				},
			},
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "ns-1",
					},
				},
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-2",
						Namespace: "ns-2",
					},
				},
			},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "group-1", Namespace: "ns-1"}},
				{NamespacedName: types.NamespacedName{Name: "group-2", Namespace: "ns-2"}},
			},
		},
		{
			name: "valid_ds_some_deleting_groups",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deviceplugin.DevicePluginName,
					Namespace: deviceplugin.DevicePluginNamespace,
				},
			},
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "ns-1",
					},
				},
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "group-2",
						Namespace:         "ns-2",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Finalizers:        []string{"dummy"},
					},
				},
			},
			want: []reconcile.Request{
				{NamespacedName: types.NamespacedName{Name: "group-1", Namespace: "ns-1"}},
			},
		},
		{
			name: "invalid_ds_name",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "wrong-name",
					Namespace: deviceplugin.DevicePluginNamespace,
				},
			},
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "ns-1",
					},
				},
			},
			want: nil,
		},
		{
			name: "invalid_ds_namespace",
			obj: &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deviceplugin.DevicePluginName,
					Namespace: "wrong-ns",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "ns-1",
					},
				},
			},
			want: nil,
		},
		{
			name: "not_a_ds",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deviceplugin.DevicePluginName,
					Namespace: deviceplugin.DevicePluginNamespace,
				},
			},
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "ns-1",
					},
				},
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()
			r := &TPUNodeGroupReconciler{
				Client: cl,
			}

			ctx := logr.NewContext(context.Background(), logr.Discard())
			got := r.mapDaemonSetToTPUNodeGroups(ctx, tc.obj)

			// Sort to compare regardless of order
			sortRequests := func(reqs []reconcile.Request) {
				if reqs == nil {
					return
				}
				for i := 0; i < len(reqs); i++ {
					for j := i + 1; j < len(reqs); j++ {
						if reqs[i].String() > reqs[j].String() {
							reqs[i], reqs[j] = reqs[j], reqs[i]
						}
					}
				}
			}

			sortRequests(got)
			sortRequests(tc.want)

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mapDaemonSetToTPUNodeGroups() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
