package tpunodegroup

import (
	"context"
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
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
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
		setupMocks        func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient)
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
				},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
			wantFinalizers: []string{
				"tpu.google.com/cleanup-mig",
				"tpu.google.com/cleanup-template",
				"tpu.google.com/cleanup-policy",
				"tpu.google.com/cleanup-nodes",
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
					BootstrapKubernetes: &tpuapi.BootstrapConfig{

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
						Project:          "test-project",
						Location:         "us-central1-a",
						InstanceTemplate: "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:       1,
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
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
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient) {
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
						Project:          "test-project",
						Location:         "us-central1-a",
						InstanceTemplate: "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:       1,
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-disabled-mig",
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							"cloud.google.com/tpu-node-group": "default-test-tpu-disabled",
						},
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
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient) {
				igm.ListManagedInstancesFunc = func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return []*computepb.ManagedInstance{
						{
							Instance:       ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/test-tpu-0"),
							InstanceStatus: ptr.To("RUNNING"),
						},
					}, nil
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
						Project:          "test-project",
						Location:         "us-central1-a",
						InstanceTemplate: "projects/test-project/global/instanceTemplates/my-template",
						TargetSize:       1,
					},
					Status: tpuapi.ManagedInstanceGroupStatus{
						URL: "projects/test-project/zones/us-central1-a/instanceGroupManagers/test-tpu-mig",
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
					Topology:     "2x2x1",
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
					Topology:     "2x2x1",
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
					Finalizers: []string{"tpu.google.com/cleanup-mig", "tpu.google.com/cleanup-template", "tpu.google.com/cleanup-policy", "tpu.google.com/cleanup-nodes"},
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							"cloud.google.com/tpu-node-group": "default-test-tpu",
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-tpu-0",
						Labels: map[string]string{
							"cloud.google.com/tpu-node-group": "default-test-tpu",
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
							"cloud.google.com/tpu-node-group": "default-test-tpu",
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
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient) {
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
			setupMocks: func(igm *gce.MockIGMClient, inst *gce.MockInstanceClient) {
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
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
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    1,
				},
			},
			wantResult:  reconcile.Result{},
			wantErr:     false,
			wantDeleted: true,
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
				objs = append(objs, &tc.nodes[i])
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
			if tc.initialObject != nil {
				builder = builder.WithStatusSubresource(tc.initialObject)
			}
			cl := builder.Build()
			k8sFakeClient := k8sfake.NewSimpleClientset()

			igm := &gce.MockIGMClient{}
			inst := &gce.MockInstanceClient{}
			if tc.setupMocks != nil {
				tc.setupMocks(igm, inst)
			}

			r := NewTPUNodeGroupReconciler(cl, scheme, k8sFakeClient, igm, inst, logr.Discard()).
				WithRecorder(record.NewFakeRecorder(10))

			gotResult, err := r.Reconcile(t.Context(), tc.request)

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
		})
	}
}

func TestTPUNodeGroupReconciler_defaultInstanceTemplate(t *testing.T) {
	expectedScript := renderStartupScript("1.31")

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

func TestTPUNodeGroupReconciler_defaultManagedInstanceGroup(t *testing.T) {
	tests := []struct {
		name  string
		mig   *tpuapi.ManagedInstanceGroup
		group *tpuapi.TPUNodeGroup
		want  *tpuapi.ManagedInstanceGroup
	}{
		{
			name:  "nil mig",
			mig:   nil,
			group: &tpuapi.TPUNodeGroup{},
			want:  nil,
		},
		{
			name: "empty topology defaults to INDIVIDUAL",
			mig: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{},
			},
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{},
			},
			want: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{
					TargetSizePolicyMode: ptr.To(tpuapi.TargetSizePolicyModeIndividual),
				},
			},
		},
		{
			name: "topology present defaults to BULK",
			mig: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{},
			},
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{
					Topology: "2x2x1",
				},
			},
			want: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{
					TargetSizePolicyMode: ptr.To(tpuapi.TargetSizePolicyModeBulk),
				},
			},
		},
		{
			name: "already set value is preserved",
			mig: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{
					TargetSizePolicyMode: ptr.To(tpuapi.TargetSizePolicyModeBulk),
				},
			},
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{
					Topology: "",
				},
			},
			want: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{
					TargetSizePolicyMode: ptr.To(tpuapi.TargetSizePolicyModeBulk),
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TPUNodeGroupReconciler{}
			r.defaultManagedInstanceGroup(tc.mig, tc.group)

			if diff := cmp.Diff(tc.want, tc.mig); diff != "" {
				t.Errorf("defaultManagedInstanceGroup() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDefaultManagedInstanceGroup(t *testing.T) {
	r := &TPUNodeGroupReconciler{}

	tests := []struct {
		name     string
		group    *tpuapi.TPUNodeGroup
		mig      *tpuapi.ManagedInstanceGroup
		wantMode string
	}{
		{
			name: "topology_empty_defaults_to_individual",
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{},
			},
			mig:      &tpuapi.ManagedInstanceGroup{},
			wantMode: "INDIVIDUAL",
		},
		{
			name: "topology_not_empty_defaults_to_bulk",
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{
					Topology: "2x2x2",
				},
			},
			mig:      &tpuapi.ManagedInstanceGroup{},
			wantMode: "BULK",
		},
		{
			name: "existing_mode_preserved",
			group: &tpuapi.TPUNodeGroup{
				Spec: tpuapi.TPUNodeGroupSpec{},
			},
			mig: &tpuapi.ManagedInstanceGroup{
				Spec: tpuapi.ManagedInstanceGroupSpec{
					TargetSizePolicyMode: ptr.To("BULK"),
				},
			},
			wantMode: "BULK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r.defaultManagedInstanceGroup(tt.mig, tt.group)
			if tt.mig.Spec.TargetSizePolicyMode == nil {
				t.Fatal("TargetSizePolicyMode is nil")
			}
			if *tt.mig.Spec.TargetSizePolicyMode != tt.wantMode {
				t.Errorf("got %s, want %s", *tt.mig.Spec.TargetSizePolicyMode, tt.wantMode)
			}
		})
	}
}
