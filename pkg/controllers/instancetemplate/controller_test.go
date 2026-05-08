package instancetemplate

import (
	"context"
	"testing"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
)

func TestInstanceTemplateReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tpuv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name           string
		request        reconcile.Request
		initialObject  *tpuv1alpha1.InstanceTemplate
		wantResult     reconcile.Result
		wantErr        bool
		wantFinalizers []string
		mockGCE        *gce.MockInstanceTemplateClient
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
			name: "resource_found_adds_finalizer",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-template",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.InstanceTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-template",
					Namespace: "default",
				},
				Spec: tpuv1alpha1.InstanceTemplateSpec{
					Project: "test-project",
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
		},
		{
			name: "resource_being_deleted_removes_finalizer",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-template",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.InstanceTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-template",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/instancetemplate-cleanup"},
				},
				Spec: tpuv1alpha1.InstanceTemplateSpec{
					Project: "test-project",
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
			mockGCE: &gce.MockInstanceTemplateClient{
				DeleteFunc: func(ctx context.Context, project, name string) (*compute.Operation, error) {
					return nil, nil // Success
				},
			},
		},
		{
			name: "resource_creation_success",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-template",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.InstanceTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-template",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
				},
				Spec: tpuv1alpha1.InstanceTemplateSpec{
					Project: "test-project",
					InstanceConfig: tpuv1alpha1.InstanceConfig{
						MachineType: "v4-8",
					},
				},
			},
			mockGCE: func() *gce.MockInstanceTemplateClient {
				var inserted bool
				return &gce.MockInstanceTemplateClient{
					GetFunc: func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
						if name == "test-template" {
							if !inserted {
								return nil, &googleapi.Error{Code: 404}
							}
							return &computepb.InstanceTemplate{
								SelfLink: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template"),
							}, nil
						}
						return nil, nil
					},
					InsertFunc: func(ctx context.Context, project string, template *computepb.InstanceTemplate) (*compute.Operation, error) {
						inserted = true
						return nil, nil
					},
				}
			}(),
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{}
			if tc.initialObject != nil {
				objs = append(objs, tc.initialObject)
			}

			builder := fake.NewClientBuilder().WithScheme(scheme)
			if tc.initialObject != nil {
				builder = builder.WithStatusSubresource(tc.initialObject)
			}
			cl := builder.WithObjects(objs...).Build()
			mockGCE := tc.mockGCE
			if mockGCE == nil {
				mockGCE = &gce.MockInstanceTemplateClient{}
			}
			r := &InstanceTemplateReconciler{
				Client:   cl,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
				GCE:      mockGCE,
			}

			gotResult, err := r.Reconcile(t.Context(), tc.request)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("Reconcile(%v) = (%v, %v), want error presence = %v", tc.request, gotResult, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
				t.Errorf("Reconcile(%v) result mismatch (-want +got):\n%s", tc.request, diff)
			}

			if tc.wantFinalizers != nil {
				var updatedObj tpuv1alpha1.InstanceTemplate
				err := cl.Get(t.Context(), tc.request.NamespacedName, &updatedObj)
				if err != nil {
					if errors.IsNotFound(err) {
						if len(tc.wantFinalizers) != 0 {
							t.Errorf("Object not found, but wanted finalizers: %v", tc.wantFinalizers)
						}
						// Success: object is gone and we wanted no finalizers.
					} else {
						t.Errorf("Failed to get updated object: %v", err)
					}
				} else {
					gotFinalizers := updatedObj.Finalizers
					if gotFinalizers == nil {
						gotFinalizers = []string{}
					}
					if diff := cmp.Diff(tc.wantFinalizers, gotFinalizers); diff != "" {
						t.Errorf("Finalizers mismatch (-want +got):\n%s", diff)
					}
				}
			}
		})
	}
}
