package deviceplugin

import (
	"testing"
)

func TestBuildDevicePluginDaemonSet(t *testing.T) {
	namespace := "test-namespace"
	ds, err := BuildDevicePluginDaemonSet(namespace)
	if err != nil {
		t.Fatalf("failed to build device plugin DaemonSet: %v", err)
	}

	if ds.Name != "tpu-device-plugin" {
		t.Errorf("expected name to be 'tpu-device-plugin', got %s", ds.Name)
	}

	if ds.Namespace != namespace {
		t.Errorf("expected namespace to be %s, got %s", namespace, ds.Namespace)
	}

	if len(ds.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ds.Spec.Template.Spec.Containers))
	}

	container := ds.Spec.Template.Spec.Containers[0]
	expectedImage := "gcr.io/gsc-nexus-xteam-shared-testing/tpu-device-plugin:latest"
	if container.Image != expectedImage {
		t.Errorf("expected image to be %s, got %s", expectedImage, container.Image)
	}

	if container.SecurityContext == nil || container.SecurityContext.SeccompProfile == nil || container.SecurityContext.SeccompProfile.Type != "RuntimeDefault" {
		t.Errorf("expected seccomp profile to be RuntimeDefault, got %v", container.SecurityContext)
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
	if exprs[0].Key != "cloud.google.com/gk8s-tpu-accelerator" || exprs[0].Operator != "Exists" {
		t.Errorf("unexpected match expression: %v", exprs[0])
	}
}
