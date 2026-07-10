package instancetemplate

import (
	"context"
	"testing"
	"time"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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

	tpuv1alpha1 "github.com/gke-labs/tpu-operator/internal/apis/tpu/v1alpha1"
	"github.com/gke-labs/tpu-operator/internal/controllers/requeue"
	"github.com/gke-labs/tpu-operator/internal/gce"
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
		wantEvents     []string
		mockGCE        *gce.MockInstanceTemplateClient
		mockGCEOps     *gce.MockGlobalOperationsClient
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
			wantEvents: []string{},
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
			wantEvents:     []string{},
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
			wantEvents:     []string{},
			mockGCE: &gce.MockInstanceTemplateClient{
				DeleteFunc: func(ctx context.Context, project, name string) (gce.Operation, error) {
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
					InsertFunc: func(ctx context.Context, project string, template *computepb.InstanceTemplate) (gce.Operation, error) {
						inserted = true
						return nil, nil
					},
				}
			}(),
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
			wantEvents: []string{
				"Normal Provisioned GCE Instance Template successfully created: https://www.googleapis.com/compute/v1/projects/test-project/global/instanceTemplates/test-template",
			},
		},
		{
			name: "resource_creation_pending_operation",
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
			mockGCE: &gce.MockInstanceTemplateClient{
				GetFunc: func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
					return nil, &googleapi.Error{Code: 404}
				},
				InsertFunc: func(ctx context.Context, project string, template *computepb.InstanceTemplate) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return false },
						NameFunc: func() string { return "op-123" },
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: requeue.LROPollInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
			wantEvents: []string{
				"Normal Provisioning GCE creation operation started: op-123",
			},
		},
		{
			name: "resource_creation_polling_operation",
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
				Status: tpuv1alpha1.InstanceTemplateStatus{
					OperationName: "op-123",
					OperationType: "CREATE",
				},
			},
			mockGCEOps: &gce.MockGlobalOperationsClient{
				GetFunc: func(ctx context.Context, project, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{Requeue: true},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
			wantEvents:     []string{},
		},
		{
			name: "creation_completed_after_deletion_requested",
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
					InstanceConfig: tpuv1alpha1.InstanceConfig{
						MachineType: "v4-8",
					},
				},
				Status: tpuv1alpha1.InstanceTemplateStatus{
					OperationName: "op-123",
					OperationType: "CREATE",
				},
			},
			mockGCEOps: &gce.MockGlobalOperationsClient{
				GetFunc: func(ctx context.Context, project, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{Requeue: true},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
			wantEvents:     []string{},
		},
		{
			name: "resource_being_deleted_starts_deletion",
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
			wantResult:     reconcile.Result{RequeueAfter: requeue.LROPollInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/instancetemplate-cleanup"},
			mockGCE: &gce.MockInstanceTemplateClient{
				DeleteFunc: func(ctx context.Context, project, name string) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return false },
						NameFunc: func() string { return "op-delete-123" },
					}, nil
				},
			},
			wantEvents: []string{
				"Normal Cleanup GCE deletion operation started: op-delete-123",
			},
		},
		{
			name: "resource_creation_operation_fails_terminally",
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
				Status: tpuv1alpha1.InstanceTemplateStatus{
					OperationName: "op-123",
					OperationType: "CREATE",
				},
			},
			mockGCEOps: &gce.MockGlobalOperationsClient{
				GetFunc: func(ctx context.Context, project, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					errCode := int32(400)
					errMessage := "Invalid configuration"
					return &computepb.Operation{
						Status:              &status,
						HttpErrorStatusCode: &errCode,
						HttpErrorMessage:    &errMessage,
						Error: &computepb.Error{
							Errors: []*computepb.Errors{
								{
									Message: ptr.To("Invalid configuration"),
								},
							},
						},
					}, nil
				},
			},
			wantResult: reconcile.Result{RequeueAfter: requeue.DriftCheckInterval},
			wantErr:    false,
			wantEvents: []string{
				"Warning OperationFailed GCE operation \"op-123\" failed: Invalid configuration (code 400): errors:{message:\"Invalid configuration\"}",
			},
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
			mockGCEOps := tc.mockGCEOps
			if mockGCEOps == nil {
				mockGCEOps = &gce.MockGlobalOperationsClient{}
			}
			fakeRecorder := record.NewFakeRecorder(10)
			r := &InstanceTemplateReconciler{
				Client:   cl,
				Scheme:   scheme,
				Recorder: fakeRecorder,
				GCE:      mockGCE,
				GCEOps:   mockGCEOps,
			}

			ctx := logr.NewContext(t.Context(), logr.Discard())
			gotResult, err := r.Reconcile(ctx, tc.request)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("Reconcile(%v) = (%v, %v), want error presence = %v", tc.request, gotResult, err, tc.wantErr)
			}

			// Compare non-duration fields first
			wantFixed := tc.wantResult
			gotFixed := gotResult
			wantFixed.RequeueAfter = 0
			gotFixed.RequeueAfter = 0
			if diff := cmp.Diff(wantFixed, gotFixed); diff != "" {
				t.Errorf("Reconcile(%v) result mismatch (-want +got):\n%s", tc.request, diff)
			}
			// Compare RequeueAfter with jitter tolerance
			if !requeue.InJitterRange(gotResult.RequeueAfter, tc.wantResult.RequeueAfter) {
				t.Errorf("Reconcile(%v) RequeueAfter = %v, want (jittered) %v", tc.request, gotResult.RequeueAfter, tc.wantResult.RequeueAfter)
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

			if tc.wantEvents != nil {
				var gotEvents []string
				for {
					select {
					case ev := <-fakeRecorder.Events:
						gotEvents = append(gotEvents, ev)
					default:
						goto DoneEvents
					}
				}
			DoneEvents:
				if diff := cmp.Diff(tc.wantEvents, gotEvents, cmpopts.EquateEmpty()); diff != "" {
					t.Errorf("Reconcile() events mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}
