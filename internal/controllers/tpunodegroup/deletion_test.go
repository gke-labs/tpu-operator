package tpunodegroup

import (
	"context"
	"testing"

	tpuapi "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/tpunodegroup/deviceplugin"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

			r := &TPUNodeGroupReconciler{Client: cl}
			got, err := r.ensureManagedInstanceGroupDeleted(context.Background(), tc.group)
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

			r := &TPUNodeGroupReconciler{Client: cl}
			got, err := r.ensureInstanceTemplateDeleted(context.Background(), tc.group)
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

			r := &TPUNodeGroupReconciler{Client: cl}
			got, err := r.ensureWorkloadPolicyDeleted(context.Background(), tc.group)
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

func TestCleanupDevicePluginIfLastGroup(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding Apps API to scheme: %v", err)
	}

	// Helper to create a DaemonSet object
	newDaemonSet := func() *appsv1.DaemonSet {
		return &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deviceplugin.DevicePluginName,
				Namespace: deviceplugin.DevicePluginNamespace,
			},
		}
	}

	tests := []struct {
		name        string
		initialObjs []client.Object
		wantResult  bool
		wantErr     bool
		verify      func(t *testing.T, cl client.Client)
	}{
		{
			name: "multiple_active_groups_no_deletion",
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-1",
						Namespace: "default",
					},
				},
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "group-2",
						Namespace: "default",
					},
				},
				newDaemonSet(),
			},
			wantResult: true,
			wantErr:    false,
			verify: func(t *testing.T, cl client.Client) {
				// Verify DaemonSet still exists
				ds := &appsv1.DaemonSet{}
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: deviceplugin.DevicePluginNamespace, Name: deviceplugin.DevicePluginName}, ds)
				if err != nil {
					t.Errorf("Expected DaemonSet to exist, got error: %v", err)
				}
			},
		},
		{
			name: "last_active_group_deleting_trigger_deletion",
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "group-1",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
				newDaemonSet(),
			},
			wantResult: true,
			wantErr:    false,
			verify: func(t *testing.T, cl client.Client) {
				// Verify DaemonSet is deleted
				ds := &appsv1.DaemonSet{}
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: deviceplugin.DevicePluginNamespace, Name: deviceplugin.DevicePluginName}, ds)
				if err == nil {
					t.Error("Expected DaemonSet to be deleted, but it still exists")
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("Expected NotFound error, got: %v", err)
				}
			},
		},
		{
			name: "concurrent_deletion_all_deleting_trigger_deletion",
			initialObjs: []client.Object{
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "group-1",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
				&tpuapi.TPUNodeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "group-2",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: metav1.Now().Time},
						Finalizers:        []string{"tpu.google.com/dummy-cleanup"},
					},
				},
				newDaemonSet(),
			},
			wantResult: true,
			wantErr:    false,
			verify: func(t *testing.T, cl client.Client) {
				// Verify DaemonSet is deleted
				ds := &appsv1.DaemonSet{}
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: deviceplugin.DevicePluginNamespace, Name: deviceplugin.DevicePluginName}, ds)
				if err == nil {
					t.Error("Expected DaemonSet to be deleted, but it still exists")
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("Expected NotFound error, got: %v", err)
				}
			},
		},
		{
			name:        "daemonset_already_deleted_no_op",
			initialObjs: []client.Object{}, // No DaemonSet, no groups
			wantResult:  true,
			wantErr:     false,
			verify: func(t *testing.T, cl client.Client) {
				// Verify DaemonSet is not found (no error other than NotFound)
				ds := &appsv1.DaemonSet{}
				err := cl.Get(context.Background(), client.ObjectKey{Namespace: deviceplugin.DevicePluginNamespace, Name: deviceplugin.DevicePluginName}, ds)
				if err == nil {
					t.Error("Expected DaemonSet to not exist")
				} else if !apierrors.IsNotFound(err) {
					t.Errorf("Expected NotFound error, got: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()

			// We pass nil logger for simplicity in tests
			r := &TPUNodeGroupReconciler{Client: cl}
			got, err := r.cleanupDevicePluginIfLastGroup(context.Background(), logr.Discard())
			if (err != nil) != tc.wantErr {
				t.Errorf("cleanupDevicePluginIfLastGroup() error = %v, wantErr %v", err, tc.wantErr)
				return
			}
			if got != tc.wantResult {
				t.Errorf("cleanupDevicePluginIfLastGroup() = %v, want %v", got, tc.wantResult)
			}

			if tc.verify != nil {
				tc.verify(t, cl)
			}
		})
	}
}

func TestDeleteBootstrapSecrets(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding CoreV1 to scheme: %v", err)
	}
	if err := tpuapi.AddToScheme(scheme); err != nil {
		t.Fatalf("Adding TPU API to scheme: %v", err)
	}

	tests := []struct {
		name          string
		group         *tpuapi.TPUNodeGroup
		initialObjs   []client.Object
		wantRemaining int
		wantErr       bool
	}{
		{
			name: "bootstrapping_disabled_no_op",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{},
			},
			initialObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "kube-system",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
			},
			wantRemaining: 1, // Should NOT be deleted because bootstrapping is disabled in spec
		},
		{
			name: "bootstrapping_enabled_deletes_secrets",
			group: &tpuapi.TPUNodeGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-tpu",
					Namespace: "default",
				},
				Spec: tpuapi.TPUNodeGroupSpec{
					BootstrapKubernetes: &tpuapi.BootstrapConfig{},
				},
			},
			initialObjs: []client.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "kube-system",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "default",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-secret",
						Namespace: "kube-system",
						Labels: map[string]string{
							labelTPUNodeGroupNamespace: "other",
							labelTPUNodeGroupName:      "test-tpu",
						},
					},
				},
			},
			wantRemaining: 1, // "other-secret" should remain
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.initialObjs...).Build()
			r := &TPUNodeGroupReconciler{Client: cl}

			err := r.deleteBootstrapSecrets(context.Background(), logr.Discard(), tc.group)
			if (err != nil) != tc.wantErr {
				t.Errorf("deleteBootstrapSecrets() error = %v, wantErr %v", err, tc.wantErr)
			}

			var secrets corev1.SecretList
			if err := cl.List(context.Background(), &secrets, client.InNamespace("kube-system")); err != nil {
				t.Fatalf("Failed to list secrets: %v", err)
			}
			if len(secrets.Items) != tc.wantRemaining {
				t.Errorf("Expected %d secrets remaining, got %d", tc.wantRemaining, len(secrets.Items))
			}
		})
	}
}
