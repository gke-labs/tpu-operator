package tpunodegroup

import (
	"context"
	"testing"

	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/google/go-cmp/cmp"
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
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
			wantErr: false,
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
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "inst-2"},
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
			wantErr: false,
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

			err := ReconcileNodes(t.Context(), cl, mockIGM, tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ReconcileNodes() error = %v, wantErr %v", err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantStatus, tc.group.Status.NodeSummary); diff != "" {
				t.Errorf("NodeSummary mismatch (-want +got):\n%s", diff)
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
		name        string
		machineType string
		topology    string
		initialNode *corev1.Node
		wantLabels  map[string]string
		wantErr     bool
	}{
		{
			name:        "label_missing",
			machineType: "ct4p-hightpu-4t",
			topology:    "2x2x2",
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
			},
			wantErr: false,
		},
		{
			name:        "labels_already_correct",
			machineType: "ct4p-hightpu-4t",
			topology:    "2x2x2",
			initialNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node",
					Labels: map[string]string{
						labelTPUAccelerator:                 "tpu-v4-podslice",
						labelTPUNodeGroup:                   "default-test-tpu",
						"cloud.google.com/gke-tpu-topology": "2x2x2",
					},
				},
			},
			wantLabels: map[string]string{
				labelTPUAccelerator:                 "tpu-v4-podslice",
				labelTPUNodeGroup:                   "default-test-tpu",
				"cloud.google.com/gke-tpu-topology": "2x2x2",
			},
			wantErr: false,
		},
		{
			name:        "accelerator_label_different_value",
			machineType: "ct4p-hightpu-4t",
			topology:    "2x2x2",
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
			},
			wantErr: false,
		},
		{
			name:        "group_label_different_value",
			machineType: "ct4p-hightpu-4t",
			topology:    "2x2x2",
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
			},
			wantErr: false,
		},
		{
			name:        "accelerator_type_unknown_skips",
			machineType: "unknown-type",
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
						MachineType: "ct4p-hightpu-4t",
					},
					Topology: "2x2x2",
				},
			}
			err := ensureNodeLabels(t.Context(), cl, tc.initialNode, group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureNodeLabels() error = %v, wantErr %v", err, tc.wantErr)
			}

			wantLabels := tc.wantLabels
			if wantLabels == nil {
				wantLabels = tc.initialNode.Labels
			}

			if diff := cmp.Diff(wantLabels, tc.initialNode.Labels); diff != "" {
				t.Errorf("Labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
