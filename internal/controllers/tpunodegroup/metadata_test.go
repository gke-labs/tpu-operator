package tpunodegroup

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/gce"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateBootstrapToken(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).Build()

	token, err := GenerateBootstrapToken(t.Context(), cl, nil)
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

	if !regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`).MatchString(token) {
		t.Errorf("Token does not match required kubeadm pattern: %s", token)
	}
}

func TestFetchCAHash(t *testing.T) {
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

	hash, err := FetchCAHash(t.Context(), cl)
	if err != nil {
		t.Fatalf("fetchCAHash() error = %v", err)
	}

	if !strings.HasPrefix(hash, "sha256:") {
		t.Errorf("Invalid hash format: %s", hash)
	}
}

func TestInjectMetadata(t *testing.T) {
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
				Name:   proto.String("inst-1"),
				Status: proto.String("RUNNING"),
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

	err := injectMetadata(t.Context(), group, cl, mockIGM, mockInstance, "ct5lp-hightpu-4t")
	if err != nil {
		t.Fatalf("injectMetadata() error = %v", err)
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
			gceInst := &computepb.Instance{
				Metadata: &computepb.Metadata{
					Items: tt.existing,
				},
			}
			got, _, _ := mergeMetadataItems(gceInst, tt.updates, nil)
			if diff := cmp.Diff(tt.want, got, protocmp.Transform()); diff != "" {
				t.Errorf("mergeMetadataItems() mismatch (-want +got):\n%s", diff)
			}
		})
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

func TestSliceMetadata(t *testing.T) {
	parseLabels := func(s string) map[string]string {
		m := make(map[string]string)
		for _, part := range strings.Split(s, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				m[kv[0]] = kv[1]
			}
		}
		return m
	}

	group := &tpuapi.TPUNodeGroup{
		Spec: tpuapi.TPUNodeGroupSpec{
			InstanceConfig: &tpuapi.InstanceConfig{
				MachineType: "ct5lp-hightpu-4t",
			},
			Topology:             "2x2x2",
			NodeCount:            2,
			TargetSizePolicyMode: tpuapi.TargetSizePolicyModeBulk,
		},
	}

	tests := []struct {
		name           string
		existingLabels string
		wantLabels     string
	}{
		{
			name:           "no existing labels",
			existingLabels: "",
			wantLabels:     "cloud.google.com/gke-tpu-accelerator=tpu-v5-lite-podslice,cloud.google.com/gke-accelerator-count=4,cloud.google.com/gke-tpu-topology=2x2x2",
		},
		{
			name:           "existing labels",
			existingLabels: "foo=bar",
			wantLabels:     "foo=bar,cloud.google.com/gke-tpu-accelerator=tpu-v5-lite-podslice,cloud.google.com/gke-accelerator-count=4,cloud.google.com/gke-tpu-topology=2x2x2",
		},
		{
			name:           "malformed labels",
			existingLabels: "foo=bar,malformed",
			wantLabels:     "foo=bar,malformed=,cloud.google.com/gke-tpu-accelerator=tpu-v5-lite-podslice,cloud.google.com/gke-accelerator-count=4,cloud.google.com/gke-tpu-topology=2x2x2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gceInst *computepb.Instance
			if tt.existingLabels != "" {
				gceInst = &computepb.Instance{
					Metadata: &computepb.Metadata{
						Items: []*computepb.Items{
							{Key: proto.String("kube-labels"), Value: proto.String(tt.existingLabels)},
						},
					},
				}
			}

			got := sliceMetadata(group, gceInst, group.Spec.InstanceConfig.MachineType)
			gotLabels, ok := got["kube-labels"]
			if !ok {
				t.Fatalf("sliceMetadata did not return kube-labels")
			}

			gotMap := parseLabels(gotLabels)
			wantMap := parseLabels(tt.wantLabels)
			if diff := cmp.Diff(wantMap, gotMap); diff != "" {
				t.Errorf("sliceMetadata() kube-labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
func TestSliceMetadata_TopologyRemoval(t *testing.T) {
	parseLabels := func(s string) map[string]string {
		m := make(map[string]string)
		for _, part := range strings.Split(s, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				m[kv[0]] = kv[1]
			}
		}
		return m
	}

	group := &tpuapi.TPUNodeGroup{
		Spec: tpuapi.TPUNodeGroupSpec{
			InstanceConfig: &tpuapi.InstanceConfig{
				MachineType: "ct5lp-hightpu-4t",
			},
			Topology:             "", // Empty topology in spec
			TargetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
		},
	}

	gceInst := &computepb.Instance{
		Metadata: &computepb.Metadata{
			Items: []*computepb.Items{
				{Key: proto.String("kube-labels"), Value: proto.String("cloud.google.com/gke-tpu-topology=2x2x2,foo=bar")},
			},
		},
	}

	got := sliceMetadata(group, gceInst, group.Spec.InstanceConfig.MachineType)
	gotLabels, ok := got["kube-labels"]
	if !ok {
		t.Fatalf("sliceMetadata did not return kube-labels")
	}

	gotMap := parseLabels(gotLabels)

	// Verify that topology is NOT in the output
	if _, ok := gotMap["cloud.google.com/gke-tpu-topology"]; ok {
		t.Errorf("Expected topology label to be removed, but it was present")
	}

	// Verify that other labels are preserved
	if val, ok := gotMap["foo"]; !ok || val != "bar" {
		t.Errorf("Expected foo=bar, got %v", val)
	}
}

func TestSliceMetadata_SingleHost(t *testing.T) {
	parseLabels := func(s string) map[string]string {
		m := make(map[string]string)
		for _, part := range strings.Split(s, ",") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 {
				m[kv[0]] = kv[1]
			}
		}
		return m
	}

	group := &tpuapi.TPUNodeGroup{
		Spec: tpuapi.TPUNodeGroupSpec{
			InstanceConfig: &tpuapi.InstanceConfig{
				MachineType: "ct5lp-hightpu-4t",
			},
			Topology:             "2x2x2",
			NodeCount:            1, // Single-host slice with topology
			TargetSizePolicyMode: tpuapi.TargetSizePolicyModeIndividual,
		},
	}

	gceInst := &computepb.Instance{
		Metadata: &computepb.Metadata{
			Items: []*computepb.Items{
				{Key: proto.String("kube-labels"), Value: proto.String("foo=bar")},
			},
		},
	}

	got := sliceMetadata(group, gceInst, group.Spec.InstanceConfig.MachineType)
	gotLabels, ok := got["kube-labels"]
	if !ok {
		t.Fatalf("sliceMetadata did not return kube-labels")
	}

	gotMap := parseLabels(gotLabels)

	// Should return tpu-v5-lite-device even though topology is 2x2x2 because it is single-host.
	if val, ok := gotMap["cloud.google.com/gke-tpu-accelerator"]; !ok || val != "tpu-v5-lite-device" {
		t.Errorf("Expected cloud.google.com/gke-tpu-accelerator to be tpu-v5-lite-device, got %s", val)
	}

	// Should still propagate the topology label to GCE metadata as specified in the spec
	if val, ok := gotMap["cloud.google.com/gke-tpu-topology"]; !ok || val != "2x2x2" {
		t.Errorf("Expected cloud.google.com/gke-tpu-topology to be 2x2x2, got %s", val)
	}
}

func TestGetOrGenerateBootstrapToken(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}

	group := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-group",
			Namespace: "test-ns",
		},
	}

	labels := map[string]string{
		labelTPUNodeGroupNamespace: group.Namespace,
		labelTPUNodeGroupName:      group.Name,
	}

	tests := []struct {
		name      string
		existing  []client.Object
		wantToken string // empty means we expect a newly generated one
	}{
		{
			name:     "no existing secrets",
			existing: nil,
		},
		{
			name: "valid existing secret",
			existing: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "valid-token",
						Namespace: "kube-system",
						Labels:    labels,
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("abcdef"),
						"token-secret": []byte("1234567890123456"),
						"expiration":   []byte(time.Now().Add(30 * time.Minute).Format(time.RFC3339)),
					},
				},
			},
			wantToken: "abcdef.1234567890123456",
		},
		{
			name: "expired secret",
			existing: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "expired-token",
						Namespace: "kube-system",
						Labels:    labels,
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("expired"),
						"token-secret": []byte("1234567890123456"),
						"expiration":   []byte(time.Now().Add(-1 * time.Minute).Format(time.RFC3339)),
					},
				},
			},
		},
		{
			name: "secret expiring soon",
			existing: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "soon-token",
						Namespace: "kube-system",
						Labels:    labels,
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("soon"),
						"token-secret": []byte("1234567890123456"),
						"expiration":   []byte(time.Now().Add(5 * time.Minute).Format(time.RFC3339)),
					},
				},
			},
		},
		{
			name: "multiple tokens, pick latest",
			existing: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "token-1",
						Namespace: "kube-system",
						Labels:    labels,
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("id1"),
						"token-secret": []byte("secret1234567890"),
						"expiration":   []byte(time.Now().Add(15 * time.Minute).Format(time.RFC3339)),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "token-2",
						Namespace: "kube-system",
						Labels:    labels,
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("id2"),
						"token-secret": []byte("secret1234567890"),
						"expiration":   []byte(time.Now().Add(45 * time.Minute).Format(time.RFC3339)),
					},
				},
			},
			wantToken: "id2.secret1234567890",
		},
		{
			name: "secret with wrong labels (other group)",
			existing: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-group-token",
						Namespace: "kube-system",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: group.Namespace,
							labelTPUNodeGroupName:      "other-group",
						},
					},
					Type: corev1.SecretType(bootstrapTokenSecretType),
					Data: map[string][]byte{
						"token-id":     []byte("otherid"),
						"token-secret": []byte("1234567890123456"),
						"expiration":   []byte(time.Now().Add(30 * time.Minute).Format(time.RFC3339)),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existing...).Build()

			token, err := GetOrGenerateBootstrapToken(t.Context(), cl, group)
			if err != nil {
				t.Fatalf("GetOrGenerateBootstrapToken() error = %v", err)
			}

			if tt.wantToken != "" {
				if token != tt.wantToken {
					t.Errorf("Expected token %s, got %s", tt.wantToken, token)
				}
			} else {
				// Verify a secret was created in the fake client
				var matchingSecrets corev1.SecretList
				if err := cl.List(t.Context(), &matchingSecrets, client.InNamespace("kube-system"), client.MatchingLabels(labels)); err != nil {
					t.Fatalf("listing matching secrets: %v", err)
				}

				found := false
				for _, s := range matchingSecrets.Items {
					tID := string(s.Data["token-id"])
					tSec := string(s.Data["token-secret"])
					if fmt.Sprintf("%s.%s", tID, tSec) == token {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Returned token %s not found in any matching secret in the client", token)
				}

				// If we had no existing valid token, we expect a new secret to have been created.
				// For the 'other-group' case, len(matchingSecrets) will be 1 (the new one).
				if len(matchingSecrets.Items) == 0 {
					t.Errorf("Expected at least 1 matching secret to exist, got 0")
				}
			}
		})
	}
}
