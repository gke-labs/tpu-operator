package tpunodegroup

import (
	"context"
	"testing"

	tpuapi "github.com/gke-labs/tpu-operator/pkg/apis/tpu/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEnsureManagedInstanceGroupDeleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}

	tests := []struct {
		name          string
		group         *tpuapi.TPUNodeGroup
		initialObjs   []client.Object
		wantResult    bool
		wantErr       bool
		checkResource func(t *testing.T, cl client.Client, name string)
	}{
		{
			name: "resource_not_found",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "resource_exists_not_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-tpu-mig",
						Namespace:  "default",
						Finalizers: []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
			checkResource: func(t *testing.T, cl client.Client, name string) {
				var mig tpuapi.ManagedInstanceGroup
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &mig)
				if err != nil {
					t.Fatalf("Failed to get MIG: %v", err)
				}
				if mig.DeletionTimestamp.IsZero() {
					t.Errorf("Expected DeletionTimestamp to be set")
				}
			},
		},
		{
			name: "resource_exists_already_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.ManagedInstanceGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-tpu-mig",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()

			got, err := ensureManagedInstanceGroupDeleted(context.Background(), cl, tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureManagedInstanceGroupDeleted() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if got != tc.wantResult {
				t.Errorf("ensureManagedInstanceGroupDeleted() = %v, want %v", got, tc.wantResult)
			}

			if tc.checkResource != nil {
				tc.checkResource(t, cl, tc.group.ManagedInstanceGroupName())
			}
		})
	}
}

func TestEnsureInstanceTemplateDeleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}

	tests := []struct {
		name          string
		group         *tpuapi.TPUNodeGroup
		initialObjs   []client.Object
		wantResult    bool
		wantErr       bool
		checkResource func(t *testing.T, cl client.Client, name string)
	}{
		{
			name: "resource_not_found",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "resource_exists_not_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-tpu-template",
						Namespace:  "default",
						Finalizers: []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
			checkResource: func(t *testing.T, cl client.Client, name string) {
				var template tpuapi.InstanceTemplate
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &template)
				if err != nil {
					t.Fatalf("Failed to get InstanceTemplate: %v", err)
				}
				if template.DeletionTimestamp.IsZero() {
					t.Errorf("Expected DeletionTimestamp to be set")
				}
			},
		},
		{
			name: "resource_exists_already_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.InstanceTemplate{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-tpu-template",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()

			got, err := ensureInstanceTemplateDeleted(context.Background(), cl, tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureInstanceTemplateDeleted() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if got != tc.wantResult {
				t.Errorf("ensureInstanceTemplateDeleted() = %v, want %v", got, tc.wantResult)
			}

			if tc.checkResource != nil {
				tc.checkResource(t, cl, tc.group.InstanceTemplateName())
			}
		})
	}
}

func TestEnsureWorkloadPolicyDeleted(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}

	tests := []struct {
		name          string
		group         *tpuapi.TPUNodeGroup
		initialObjs   []client.Object
		wantResult    bool
		wantErr       bool
		checkResource func(t *testing.T, cl client.Client, name string)
	}{
		{
			name: "resource_not_found",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name: "resource_exists_not_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.WorkloadPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-tpu-policy",
						Namespace:  "default",
						Finalizers: []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
			checkResource: func(t *testing.T, cl client.Client, name string) {
				var policy tpuapi.WorkloadPolicy
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: "default", Name: name}, &policy)
				if err != nil {
					t.Fatalf("Failed to get WorkloadPolicy: %v", err)
				}
				if policy.DeletionTimestamp.IsZero() {
					t.Errorf("Expected DeletionTimestamp to be set")
				}
			},
		},
		{
			name: "resource_exists_already_deleting",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
			},
			initialObjs: []client.Object{
				&tpuapi.WorkloadPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-tpu-policy",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
			},
			wantResult: false,
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()

			got, err := ensureWorkloadPolicyDeleted(context.Background(), cl, tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("ensureWorkloadPolicyDeleted() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if got != tc.wantResult {
				t.Errorf("ensureWorkloadPolicyDeleted() = %v, want %v", got, tc.wantResult)
			}

			if tc.checkResource != nil {
				tc.checkResource(t, cl, tc.group.WorkloadPolicyName())
			}
		})
	}
}
