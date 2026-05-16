package deviceplugin

import (
	"context"
	"testing"

	tpuapi "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestBuildDevicePluginDaemonSet(t *testing.T) {
	group := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
		},
	}
	ds, err := BuildDevicePluginDaemonSet(group)
	if err != nil {
		t.Fatalf("failed to build device plugin DaemonSet: %v", err)
	}

	if ds.Name != "tpu-device-plugin" {
		t.Errorf("expected name to be 'tpu-device-plugin', got %s", ds.Name)
	}

	if ds.Namespace != group.Namespace {
		t.Errorf("expected namespace to be %s, got %s", group.Namespace, ds.Namespace)
	}

	if len(ds.Spec.Template.Spec.Containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(ds.Spec.Template.Spec.Containers))
	}

	container := ds.Spec.Template.Spec.Containers[0]
	expectedImage := "gcr.io/gke-release/tpu-device-plugin:1.35.7-gke.0"
	if container.Image != expectedImage {
		t.Errorf("expected image to be %s, got %s", expectedImage, container.Image)
	}

	if container.SecurityContext == nil || container.SecurityContext.SeccompProfile == nil || container.SecurityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Errorf("expected seccomp profile to be RuntimeDefault, got %v", container.SecurityContext)
	}

	sidecar := ds.Spec.Template.Spec.Containers[1]
	expectedSidecarImage := "gcr.io/gke-release/gke-distroless/bash@sha256:f97677214e19917c800bd7165a42bf5dbe1e5afde513aecb48a4916c145e1504"
	if sidecar.Image != expectedSidecarImage {
		t.Errorf("expected sidecar image to be %s, got %s", expectedSidecarImage, sidecar.Image)
	}

	vbarAgent := ds.Spec.Template.Spec.Containers[2]
	expectedVbarImage := "gcr.io/gke-release/vbar_control_agent@sha256:450e948cfa0b0db0c0136138f96da19589635409e6eb790b80fcaee3ffc6cd75"
	if vbarAgent.Image != expectedVbarImage {
		t.Errorf("expected vbar agent image to be %s, got %s", expectedVbarImage, vbarAgent.Image)
	}

	affinity := ds.Spec.Template.Spec.Affinity
	if affinity == nil || affinity.NodeAffinity == nil || affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		t.Fatalf("expected node affinity to be set")
	}
	terms := affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	if len(terms) != 1 {
		t.Fatalf("expected 1 node selector term, got %d", len(terms))
	}
	exprs := terms[0].MatchExpressions
	if len(exprs) != 1 {
		t.Fatalf("expected 1 match expression, got %d", len(exprs))
	}
	if exprs[0].Key != "cloud.google.com/gke-tpu-accelerator" || exprs[0].Operator != "Exists" {
		t.Errorf("unexpected match expression: %v", exprs[0])
	}
}

func TestReconcile(t *testing.T) {
	group := &tpuapi.TPUNodeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-tpu",
			Namespace: "default",
		},
	}

	t.Run("creates DaemonSet when not exists", func(t *testing.T) {
		k8sFakeClient := k8sfake.NewSimpleClientset()

		err := Reconcile(context.Background(), k8sFakeClient, group)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		ds, err := k8sFakeClient.AppsV1().DaemonSets("default").Get(context.Background(), "tpu-device-plugin", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get DaemonSet: %v", err)
		}
		if ds.Name != "tpu-device-plugin" {
			t.Errorf("expected name to be 'tpu-device-plugin', got %s", ds.Name)
		}

		sa, err := k8sFakeClient.CoreV1().ServiceAccounts("kube-system").Get(context.Background(), "tpu-device-plugin-sa", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get ServiceAccount: %v", err)
		}
		if sa.Name != "tpu-device-plugin-sa" {
			t.Errorf("expected name to be 'tpu-device-plugin-sa', got %s", sa.Name)
		}
	})

	t.Run("does nothing when DaemonSet already exists", func(t *testing.T) {
		k8sFakeClient := k8sfake.NewSimpleClientset()

		// Pre-create the DaemonSet
		ds, err := BuildDevicePluginDaemonSet(group)
		if err != nil {
			t.Fatalf("failed to build device plugin DaemonSet: %v", err)
		}
		_, err = k8sFakeClient.AppsV1().DaemonSets("default").Create(context.Background(), ds, metav1.CreateOptions{})
		if err != nil {
			t.Fatalf("failed to pre-create DaemonSet: %v", err)
		}

		err = Reconcile(context.Background(), k8sFakeClient, group)
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}

		// Verify it still exists and wasn't duplicated or modified in a bad way (though our current logic doesn't update).
		_, err = k8sFakeClient.AppsV1().DaemonSets("default").Get(context.Background(), "tpu-device-plugin", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get DaemonSet: %v", err)
		}
	})
}
