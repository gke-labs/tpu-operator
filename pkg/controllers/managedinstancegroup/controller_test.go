package managedinstancegroup

import (
	"context"
	"fmt"
	"testing"
	"time"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/api/googleapi"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
	"gke-internal.googlesource.com/tpu-node-group/pkg/gce"
)

func TestManagedInstanceGroupReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tpuv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name           string
		request        reconcile.Request
		initialObject  *tpuv1alpha1.ManagedInstanceGroup
		wantResult     reconcile.Result
		wantErr        bool
		wantFinalizers []string
		mockGCE        *gce.MockIGMClient
		mockGCEOps     *gce.MockZoneOperationsClient
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
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "resource_being_deleted_removes_finalizer",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
		},
		{
			name: "resource_creation_success",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
					InstanceTemplate: "test-template",
					TargetSize:       1,
				},
			},
			mockGCE: func() *gce.MockIGMClient {
				var inserted bool
				return &gce.MockIGMClient{
					GetFunc: func(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error) {
						if name == "test-mig" {
							if !inserted {
								return nil, &googleapi.Error{Code: 404}
							}
							return &computepb.InstanceGroupManager{
								SelfLink: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/zones/us-central1-a/instanceGroupManagers/test-mig"),
							}, nil
						}
						return nil, nil
					},
					InsertFunc: func(ctx context.Context, project, zone string, igm *computepb.InstanceGroupManager) (gce.Operation, error) {
						inserted = true
						return nil, nil
					},
				}
			}(),
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "resource_creation_pending_operation",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
					InstanceTemplate: "test-template",
					TargetSize:       1,
				},
			},
			mockGCE: &gce.MockIGMClient{
				GetFunc: func(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error) {
					return nil, &googleapi.Error{Code: 404}
				},
				InsertFunc: func(ctx context.Context, project, zone string, igm *computepb.InstanceGroupManager) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return false },
						NameFunc: func() string { return "op-123" },
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "resource_creation_polling_operation",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
					InstanceTemplate: "test-template",
					TargetSize:       1,
				},
				Status: tpuv1alpha1.ManagedInstanceGroupStatus{
					OperationName: "op-123",
				},
			},
			mockGCEOps: &gce.MockZoneOperationsClient{
				GetFunc: func(ctx context.Context, project, zone, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{Requeue: true},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "creation_gce_get_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				GetFunc: func(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error) {
					return nil, fmt.Errorf("forced GCE error")
				},
			},
			wantErr: true,
		},
		{
			name: "creation_gce_insert_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				GetFunc: func(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error) {
					return nil, &googleapi.Error{Code: 404}
				},
				InsertFunc: func(ctx context.Context, project, zone string, igm *computepb.InstanceGroupManager) (gce.Operation, error) {
					return nil, fmt.Errorf("forced insert error")
				},
			},
			wantErr: true,
		},
		{
			name: "polling_operation_still_pending",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Status: tpuv1alpha1.ManagedInstanceGroupStatus{
					OperationName: "op-123",
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCEOps: &gce.MockZoneOperationsClient{
				GetFunc: func(ctx context.Context, project, zone, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_RUNNING
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult: reconcile.Result{RequeueAfter: 10 * time.Second},
			wantErr:    false,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "polling_operation_failed",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mig",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Status: tpuv1alpha1.ManagedInstanceGroupStatus{
					OperationName: "op-123",
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCEOps: &gce.MockZoneOperationsClient{
				GetFunc: func(ctx context.Context, project, zone, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status:               &status,
						HttpErrorStatusCode: ptr.To(int32(500)),
						HttpErrorMessage:     ptr.To("internal error"),
					}, nil
				},
			},
			wantErr: true,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "deletion_gce_delete_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				DeleteFunc: func(ctx context.Context, project, zone, name string) (gce.Operation, error) {
					return nil, fmt.Errorf("forced delete error")
				},
			},
			wantErr: true,
			wantFinalizers: []string{"tpu.google.com/managedinstancegroup-cleanup"},
		},
		{
			name: "deletion_gce_not_found",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				DeleteFunc: func(ctx context.Context, project, zone, name string) (gce.Operation, error) {
					return nil, &googleapi.Error{Code: 404}
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
		},
		{
			name: "polling_delete_operation_404",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Status: tpuv1alpha1.ManagedInstanceGroupStatus{
					OperationName: "op-123",
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCEOps: &gce.MockZoneOperationsClient{
				GetFunc: func(ctx context.Context, project, zone, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status:               &status,
						HttpErrorStatusCode: ptr.To(int32(404)),
					}, nil
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
		},
		{
			name: "deletion_operation_immediately_done",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				DeleteFunc: func(ctx context.Context, project, zone, name string) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return true },
					}, nil
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
		},
		{
			name: "deletion_no_operation",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-mig",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.ManagedInstanceGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-mig",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/managedinstancegroup-cleanup"},
				},
				Spec: tpuv1alpha1.ManagedInstanceGroupSpec{
					Project:          "test-project",
					Location:         "us-central1-a",
				},
			},
			mockGCE: &gce.MockIGMClient{
				DeleteFunc: func(ctx context.Context, project, zone, name string) (gce.Operation, error) {
					return nil, nil
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
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
				mockGCE = &gce.MockIGMClient{}
			}
			mockGCEOps := tc.mockGCEOps
			if mockGCEOps == nil {
				mockGCEOps = &gce.MockZoneOperationsClient{}
			}
			r := &ManagedInstanceGroupReconciler{
				Client: cl,
				Scheme: scheme,
				Log:    logr.Discard(),
				GCE:    mockGCE,
				GCEOps: mockGCEOps,
			}

			gotResult, err := r.Reconcile(t.Context(), tc.request)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("Reconcile(%v) = (%v, %v), want error presence = %v", tc.request, gotResult, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
				t.Errorf("Reconcile(%v) result mismatch (-want +got):\n%s", tc.request, diff)
			}

			if tc.wantFinalizers != nil {
				var updatedObj tpuv1alpha1.ManagedInstanceGroup
				err := cl.Get(t.Context(), tc.request.NamespacedName, &updatedObj)
				if err != nil {
					if errors.IsNotFound(err) {
						if len(tc.wantFinalizers) != 0 {
							t.Errorf("Object not found, but wanted finalizers: %v", tc.wantFinalizers)
						}
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
