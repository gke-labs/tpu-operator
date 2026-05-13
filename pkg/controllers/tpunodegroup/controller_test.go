package tpunodegroup

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
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
		name          string
		request       reconcile.Request
		initialObject *tpuapi.TPUNodeGroup
		wantResult    reconcile.Result
		wantErr       bool
		wantDaemonSet bool
		wantStatus    *tpuapi.NodeSummary
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
			name: "resource_found_success",
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
			wantResult:    reconcile.Result{},
			wantErr:       false,
			wantDaemonSet: true,
			wantStatus: &tpuapi.NodeSummary{
				Total:       1,
				Ready:       0,
				Reconciling: 1,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{}
			if tc.initialObject != nil {
				objs = append(objs, tc.initialObject)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
			if tc.initialObject != nil {
				builder = builder.WithStatusSubresource(tc.initialObject)
			}
			cl := builder.Build()
			k8sFakeClient := k8sfake.NewSimpleClientset()

			r := NewTPUNodeGroupReconciler(cl, scheme, k8sFakeClient, &gce.MockIGMClient{}, &gce.MockInstanceClient{}, logr.Discard()).
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
				_, err := k8sFakeClient.AppsV1().DaemonSets("default").Get(t.Context(), "tpu-device-plugin", metav1.GetOptions{})
				if err != nil {
					if errors.IsNotFound(err) {
						t.Errorf("Expected DaemonSet 'tpu-device-plugin' to be created, but it was not found")
					} else {
						t.Errorf("Failed to get DaemonSet: %v", err)
					}
				}
			}

			if tc.wantStatus != nil {
				var updatedObject tpuapi.TPUNodeGroup
				if err := cl.Get(t.Context(), tc.request.NamespacedName, &updatedObject); err != nil {
					t.Fatalf("Failed to get updated object: %v", err)
				}
				if diff := cmp.Diff(tc.wantStatus, updatedObject.Status.NodeSummary); diff != "" {
					t.Errorf("Status.NodeSummary mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestTPUNodeGroupReconciler_defaultInstanceTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template *tpuapi.InstanceTemplate
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &TPUNodeGroupReconciler{}
			r.defaultInstanceTemplate(tc.template)

			if diff := cmp.Diff(tc.want, tc.template); diff != "" {
				t.Errorf("defaultInstanceTemplate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

