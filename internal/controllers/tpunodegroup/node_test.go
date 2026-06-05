package tpunodegroup

import (
	"context"
	"testing"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/gce"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeManager_ReconcileNodes(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	tests := []struct {
		name             string
		group            *tpuapi.TPUNodeGroup
		nodes            []corev1.Node
		managedInstances []*computepb.ManagedInstance
		wantStatus       *tpuapi.NodeSummary
		wantEvents       []string
		wantErr          bool
	}{
		{
			name: "all_nodes_joined_and_ready",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    2,
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-1"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-1",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-2",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			managedInstances: []*computepb.ManagedInstance{
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-2")},
			},
			wantStatus: &tpuapi.NodeSummary{
				Ready:       2,
				Reconciling: 0,
			},
			wantEvents: nil,
			wantErr:    false,
		},
		{
			name: "one_node_ready_one_pending",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    2,
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-1"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-1",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-2",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			managedInstances: []*computepb.ManagedInstance{
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-2")},
			},
			wantStatus: &tpuapi.NodeSummary{
				Ready:       1,
				Reconciling: 1,
			},
			wantEvents: []string{
				"Normal NodesJoining Waiting for 1 of 2 nodes to join the cluster",
			},
			wantErr: false,
		},
		{
			name: "event_emitted_on_joining_progress",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    3,
				},
				Status: tpuapi.TPUNodeGroupStatus{
					NodeSummary: &tpuapi.NodeSummary{
						Ready: 1,
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-1"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-1",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-2",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-3"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-3",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			managedInstances: []*computepb.ManagedInstance{
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-2")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-3")},
			},
			wantStatus: &tpuapi.NodeSummary{
				Ready:       2,
				Reconciling: 1,
			},
			wantEvents: []string{
				"Normal NodesJoining Waiting for 1 of 3 nodes to join the cluster",
			},
			wantErr: false,
		},
		{
			name: "no_event_on_no_progress",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    3,
				},
				Status: tpuapi.TPUNodeGroupStatus{
					NodeSummary: &tpuapi.NodeSummary{
						Ready: 1,
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-1"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-1",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-2",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-3"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-3",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			managedInstances: []*computepb.ManagedInstance{
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-2")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-3")},
			},
			wantStatus: &tpuapi.NodeSummary{
				Ready:       1,
				Reconciling: 2,
			},
			wantEvents: nil,
			wantErr:    false,
		},
		{
			name: "no_event_on_all_ready",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					Project:      "test-project",
					NodeLocation: "us-central1-a",
					NodeCount:    2,
				},
				Status: tpuapi.TPUNodeGroupStatus{
					NodeSummary: &tpuapi.NodeSummary{
						Ready: 1,
					},
				},
			},
			nodes: []corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-1"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-1",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
					Spec: corev1.NodeSpec{
						ProviderID: "gce://test-project/us-central1-a/inst-2",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			managedInstances: []*computepb.ManagedInstance{
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1")},
				{Instance: proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-2")},
			},
			wantStatus: &tpuapi.NodeSummary{
				Ready:       2,
				Reconciling: 0,
			},
			wantEvents: nil,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{tc.group}
			for i := range tc.nodes {
				objs = append(objs, &tc.nodes[i])
			}

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(tc.group).Build()

			mockIGM := &gce.MockIGMClient{
				ListManagedInstancesFunc: func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
					return tc.managedInstances, nil
				},
			}

			recorder := record.NewFakeRecorder(10)
			err := ReconcileNodes(t.Context(), cl, mockIGM, recorder, tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ReconcileNodes() error = %v, wantErr %v", err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantStatus, tc.group.Status.NodeSummary); diff != "" {
				t.Errorf("NodeSummary mismatch (-want +got):\n%s", diff)
			}

			var gotEvents []string
			close(recorder.Events)
			for e := range recorder.Events {
				gotEvents = append(gotEvents, e)
			}
			if diff := cmp.Diff(tc.wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Emitted events mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEnsureNodeLabel(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	tests := []struct {
		name                 string
		machineType          string
		topology             string
		targetSizePolicyMode string
		initialNode          *corev1.Node
		wantLabels           map[string]string
		wantErr              bool
	}{
		{
			name:                 "label_missing",
			machineType:          "ct4p-hightpu-4t",
			topology:             "2x2x2",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
				labelTPUAcceleratorCount:            "4",
			},
			wantErr: false,
		},
		{
			name:                 "labels_already_correct",
			machineType:          "ct4p-hightpu-4t",
			topology:             "2x2x2",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						labelTPUAccelerator:                 "tpu-v4-podslice",
						labelTPUNodeGroup:                   "default-test-tpu",
						"cloud.google.com/gke-tpu-topology": "2x2x2",
						labelTPUAcceleratorCount:            "4",
					},
				},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
				labelTPUAcceleratorCount:            "4",
			},
			wantErr: false,
		},
		{
			name:                 "accelerator_label_different_value",
			machineType:          "ct4p-hightpu-4t",
			topology:             "2x2x2",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: map[string]string{labelTPUAccelerator: "false"},
				},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
				labelTPUAcceleratorCount:            "4",
			},
			wantErr: false,
		},
		{
			name:                 "group_label_different_value",
			machineType:          "ct4p-hightpu-4t",
			topology:             "2x2x2",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						labelTPUAccelerator:                 "tpu-v4-podslice",
						labelTPUNodeGroup:                   "wrong-value",
						"cloud.google.com/gke-tpu-topology": "2x2x2",
					},
				},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
				labelTPUAcceleratorCount:            "4",
			},
			wantErr: false,
		},
		{
			name:                 "accelerator_type_unknown_skips",
			machineType:          "unknown-type",
			targetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			},
			wantLabels: nil, // Should not be updated
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialNode).Build()

			group := &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					InstanceConfig: &tpuapi.InstanceConfig{
						MachineType: tc.machineType,
					},
					Topology:             tc.topology,
					NodeCount:            2,
					TargetSizePolicyMode: tc.targetSizePolicyMode,
				},
			}

			// Save initial labels for "no change" assertion
			var initialLabels map[string]string
			if tc.initialNode.Labels != nil {
				initialLabels = make(map[string]string)
				for k, v := range tc.initialNode.Labels {
					initialLabels[k] = v
				}
			}

			err := ensureNodeLabels(t.Context(), cl, tc.initialNode, group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureNodeLabels() error = %v, wantErr %v", err, tc.wantErr)
			}

			wantLabels := tc.wantLabels
			if wantLabels == nil {
				wantLabels = initialLabels
			}

			if diff := cmp.Diff(wantLabels, tc.initialNode.Labels); diff != "" {
				t.Errorf("Labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
