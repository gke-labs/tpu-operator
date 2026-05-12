package tpunodegroup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/google/go-cmp/cmp"
	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"google.golang.org/protobuf/proto"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNodeBootstrapper_ReconcileNodeJoin(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	tests := []struct {
		name              string
		group             *tpuapi.TPUNodeGroup
		nodes             []corev1.Node
		managedInstances  []*computepb.ManagedInstance
		wantStatus        *tpuapi.NodeSummary
		wantErr           bool
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
				Total:       2,
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
				Total:       2,
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

			bootstrapper := NewNodeBootstrapper(cl, mockIGM, nil)

			err := bootstrapper.ReconcileNodeJoin(t.Context(), tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ReconcileNodeJoin() error = %v, wantErr %v", err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantStatus, tc.group.Status.NodeSummary); diff != "" {
				t.Errorf("NodeSummary mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNodeBootstrapper_generateBootstrapToken(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()
	bootstrapper := NewNodeBootstrapper(cl, nil, nil)

	token, err := bootstrapper.generateBootstrapToken(t.Context())
	if err != nil {
		t.Fatalf("generateBootstrapToken() error = %v", err)
	}

	if token == "" {
		t.Errorf("generateBootstrapToken() returned empty token")
	}

	var secretList corev1.SecretList
	if err := cl.List(t.Context(), &secretList, client.InNamespace("kube-system")); err != nil {
		t.Fatalf("Failed to list secrets: %v", err)
	}

	if len(secretList.Items) != 1 {
		t.Errorf("Expected 1 secret, got %d", len(secretList.Items))
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Errorf("Invalid token format: %s", token)
	}
}

func TestNodeBootstrapper_getCAHash(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	caCertPEM := generateTestCACert(t)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"ca.crt": caCertPEM,
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	bootstrapper := NewNodeBootstrapper(cl, nil, nil)

	hash, err := bootstrapper.getCAHash(t.Context())
	if err != nil {
		t.Fatalf("getCAHash() error = %v", err)
	}

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("Invalid hash format: %s", hash)
	}
}

func generateTestCACert(t *testing.T) string {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(1 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	return string(pemBytes)
}


func TestNodeBootstrapper_InjectJoinTokens(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	caCertPEM := generateTestCACert(t)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-root-ca.crt",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"ca.crt": caCertPEM,
		},
	}

	group := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tpu",
			Namespace: "default",
		},
		Spec: tpuapi.TPUNodeGroupSpec{
			Project:      "test-project",
			NodeLocation: "us-central1-a",
			NodeCount:    1,
			BootstrapKubernetes: &tpuapi.BootstrapConfig{

				ControlPlaneIP: "1.2.3.4",
			},
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(group, cm).Build()

	mockIGM := &gce.MockIGMClient{
		ListManagedInstancesFunc: func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
			return []*computepb.ManagedInstance{
				{
					Instance:       proto.String("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instances/inst-1"),
					InstanceStatus: proto.String("RUNNING"),
				},
			}, nil
		},
	}

	var gotMetadata *computepb.Metadata
	mockInstance := &gce.MockInstanceClient{
		GetFunc: func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
			return &computepb.Instance{
				Name: proto.String("inst-1"),
				Metadata: &computepb.Metadata{
					Fingerprint: proto.String("xyz"),
					Items: []*computepb.Items{
						{Key: proto.String("existing-key"), Value: proto.String("existing-value")},
					},
				},
			}, nil
		},
		SetMetadataFunc: func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
			gotMetadata = req.MetadataResource
			return nil, nil
		},
	}

	bootstrapper := NewNodeBootstrapper(cl, mockIGM, mockInstance)

	err := bootstrapper.InjectJoinTokens(t.Context(), group)
	if err != nil {
		t.Fatalf("InjectJoinTokens() error = %v", err)
	}

	if gotMetadata == nil {
		t.Fatal("SetMetadata was not called")
	}

	expectedKeys := map[string]bool{
		"existing-key":             true,
		"kubeadm-join-token":       true,
		"kubeadm-control-plane-ip": true,
		"kubeadm-ca-hash":          true,
	}

	for _, item := range gotMetadata.Items {
		delete(expectedKeys, *item.Key)
		if *item.Key == "kubeadm-control-plane-ip" && *item.Value != "1.2.3.4" {
			t.Errorf("Expected CP IP 1.2.3.4, got %s", *item.Value)
		}
	}

	if len(expectedKeys) != 0 {
		t.Errorf("Missing expected metadata keys: %v", expectedKeys)
	}
}

func TestMergeMetadataItems(t *testing.T) {
	tests := []struct {
		name     string
		existing []*computepb.Items
		updates  map[string]string
		want     []*computepb.Items
	}{
		{
			name:     "empty existing",
			existing: nil,
			updates:  map[string]string{"key1": "val1"},
			want: []*computepb.Items{
				{Key: proto.String("key1"), Value: proto.String("val1")},
			},
		},
		{
			name: "overwrite existing",
			existing: []*computepb.Items{
				{Key: proto.String("key1"), Value: proto.String("oldval")},
			},
			updates: map[string]string{"key1": "newval"},
			want: []*computepb.Items{
				{Key: proto.String("key1"), Value: proto.String("newval")},
			},
		},
		{
			name: "preserve disjoint",
			existing: []*computepb.Items{
				{Key: proto.String("key1"), Value: proto.String("val1")},
			},
			updates: map[string]string{"key2": "val2"},
			want: []*computepb.Items{
				{Key: proto.String("key1"), Value: proto.String("val1")},
				{Key: proto.String("key2"), Value: proto.String("val2")},
			},
		},
		{
			name: "skip nil key",
			existing: []*computepb.Items{
				{Key: nil, Value: proto.String("val1")},
			},
			updates: map[string]string{"key2": "val2"},
			want: []*computepb.Items{
				{Key: proto.String("key2"), Value: proto.String("val2")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeMetadataItems(tt.existing, tt.updates)
			if len(got) != len(tt.want) {
				t.Errorf("mergeMetadataItems() returned %d items, want %d", len(got), len(tt.want))
			}
			for _, w := range tt.want {
				found := false
				for _, g := range got {
					if g.Key != nil && w.Key != nil && *g.Key == *w.Key {
						if g.Value != nil && w.Value != nil && *g.Value != *w.Value {
							t.Errorf("For key %s, got value %s, want %s", *g.Key, *g.Value, *w.Value)
						}
						found = true
						break
					}
				}
				if !found && w.Key != nil {
					t.Errorf("Expected key %s not found in result", *w.Key)
				}
			}
		})
	}
}

