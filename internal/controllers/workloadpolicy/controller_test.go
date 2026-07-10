package workloadpolicy

import (
	"context"
	"fmt"
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

func TestWorkloadPolicyReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tpuv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name           string
		request        reconcile.Request
		initialObject  *tpuv1alpha1.WorkloadPolicy
		wantResult     reconcile.Result
		wantErr        bool
		wantFinalizers []string
		wantEvents     []string
		mockGCE        *gce.MockResourcePolicyClient
		mockGCEOps     *gce.MockRegionOperationsClient
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
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-policy",
					Namespace: "default",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "v4-8",
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
		},
		{
			name: "resource_being_deleted_removes_finalizer",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
					return nil, nil // Success
				},
			},
		},
		{
			name: "resource_creation_success",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "v4-8",
				},
			},
			mockGCE: func() *gce.MockResourcePolicyClient {
				var inserted bool
				return &gce.MockResourcePolicyClient{
					GetFunc: func(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error) {
						if name == "test-policy" {
							if !inserted {
								return nil, &googleapi.Error{Code: 404}
							}
							return &computepb.ResourcePolicy{
								SelfLink: ptr.To("https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/resourcePolicies/test-policy"),
							}, nil
						}
						return nil, nil
					},
					InsertFunc: func(ctx context.Context, project, region string, policy *computepb.ResourcePolicy) (gce.Operation, error) {
						inserted = true
						return nil, nil
					},
				}
			}(),
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
			wantEvents: []string{
				"Normal Provisioned GCE resource successfully created: https://www.googleapis.com/compute/v1/projects/test-project/regions/us-central1/resourcePolicies/test-policy",
			},
		},
		{
			name: "resource_creation_pending_operation",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "v4-8",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				GetFunc: func(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error) {
					return nil, &googleapi.Error{Code: 404}
				},
				InsertFunc: func(ctx context.Context, project, region string, policy *computepb.ResourcePolicy) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return false },
						NameFunc: func() string { return "op-123" },
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: requeue.LROPollInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
			wantEvents: []string{
				"Normal Provisioning GCE creation operation started: op-123",
			},
		},
		{
			name: "resource_creation_polling_operation",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "v4-8",
				},
				Status: tpuv1alpha1.WorkloadPolicyStatus{
					OperationName: "op-123",
					OperationType: "CREATE",
				},
			},
			mockGCEOps: &gce.MockRegionOperationsClient{
				GetFunc: func(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{Requeue: true},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
		},
		{
			name: "creation_completed_after_deletion_requested",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project:             "test-project",
					Region:              "us-central1",
					AcceleratorTopology: "v4-8",
				},
				Status: tpuv1alpha1.WorkloadPolicyStatus{
					OperationName: "op-123",
					OperationType: "CREATE",
				},
			},
			mockGCEOps: &gce.MockRegionOperationsClient{
				GetFunc: func(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{Requeue: true},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
		},
		{
			name: "creation_gce_get_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				GetFunc: func(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error) {
					return nil, fmt.Errorf("forced GCE error")
				},
			},
			wantErr: true,
		},
		{
			name: "creation_gce_insert_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				GetFunc: func(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error) {
					return nil, &googleapi.Error{Code: 404}
				},
				InsertFunc: func(ctx context.Context, project, region string, policy *computepb.ResourcePolicy) (gce.Operation, error) {
					return nil, fmt.Errorf("forced insert error")
				},
			},
			wantErr: true,
		},
		{
			name: "polling_operation_still_pending",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Status: tpuv1alpha1.WorkloadPolicyStatus{
					OperationName: "op-123",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCEOps: &gce.MockRegionOperationsClient{
				GetFunc: func(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_RUNNING
					return &computepb.Operation{
						Status: &status,
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: requeue.LROPollInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
		},
		{
			name: "polling_operation_failed",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-policy",
					Namespace:  "default",
					Finalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Status: tpuv1alpha1.WorkloadPolicyStatus{
					OperationName: "op-123",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCEOps: &gce.MockRegionOperationsClient{
				GetFunc: func(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status:              &status,
						HttpErrorStatusCode: ptr.To(int32(500)),
						HttpErrorMessage:    ptr.To("internal error"),
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: requeue.DriftCheckInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
			wantEvents: []string{
				"Warning OperationFailed GCE operation \"op-123\" failed: internal error (code 500): <nil>",
			},
		},
		{
			name: "deletion_gce_delete_error",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
					return nil, fmt.Errorf("forced delete error")
				},
			},
			wantErr:        true,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
		},
		{
			name: "deletion_gce_not_found",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
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
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Status: tpuv1alpha1.WorkloadPolicyStatus{
					OperationName: "op-123",
					OperationType: "DELETE",
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCEOps: &gce.MockRegionOperationsClient{
				GetFunc: func(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
					status := computepb.Operation_DONE
					return &computepb.Operation{
						Status:              &status,
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
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
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
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
					return nil, nil
				},
			},
			wantResult:     reconcile.Result{},
			wantErr:        false,
			wantFinalizers: []string{},
		},
		{
			name: "deletion_gce_delete_starts",
			request: reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-policy",
					Namespace: "default",
				},
			},
			initialObject: &tpuv1alpha1.WorkloadPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-policy",
					Namespace:         "default",
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{"tpu.google.com/workloadpolicy-cleanup"},
				},
				Spec: tpuv1alpha1.WorkloadPolicySpec{
					Project: "test-project",
					Region:  "us-central1",
				},
			},
			mockGCE: &gce.MockResourcePolicyClient{
				DeleteFunc: func(ctx context.Context, project, region, name string) (gce.Operation, error) {
					return &gce.MockOperation{
						DoneFunc: func() bool { return false },
						NameFunc: func() string { return "op-delete-123" },
					}, nil
				},
			},
			wantResult:     reconcile.Result{RequeueAfter: requeue.LROPollInterval},
			wantErr:        false,
			wantFinalizers: []string{"tpu.google.com/workloadpolicy-cleanup"},
			wantEvents: []string{
				"Normal Cleanup GCE deletion operation started: op-delete-123",
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
				mockGCE = &gce.MockResourcePolicyClient{}
			}
			mockGCEOps := tc.mockGCEOps
			if mockGCEOps == nil {
				mockGCEOps = &gce.MockRegionOperationsClient{}
			}
			fakeRecorder := record.NewFakeRecorder(10)
			r := &WorkloadPolicyReconciler{
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
				var updatedObj tpuv1alpha1.WorkloadPolicy
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

type recordingSink struct {
	logs []string
}

func (s *recordingSink) Init(info logr.RuntimeInfo) {}
func (s *recordingSink) Enabled(level int) bool     { return true }
func (s *recordingSink) Info(level int, msg string, keysAndValues ...interface{}) {
	s.logs = append(s.logs, msg)
}
func (s *recordingSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.logs = append(s.logs, msg)
}
func (s *recordingSink) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return s
}
func (s *recordingSink) WithName(name string) logr.LogSink {
	return s
}

func TestWorkloadPolicyReconciler_Reconcile_UsesContextLogger(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := tpuv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	wp := &tpuv1alpha1.WorkloadPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-policy",
			Namespace: "default",
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wp).Build()

	r := &WorkloadPolicyReconciler{
		Client:   cl,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(10),
		GCE:      &gce.MockResourcePolicyClient{},
		GCEOps:   &gce.MockRegionOperationsClient{},
	}

	sink := &recordingSink{}
	logger := logr.New(sink)
	ctx := logr.NewContext(t.Context(), logger)

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-policy",
			Namespace: "default",
		},
	}

	_, _ = r.Reconcile(ctx, req)

	expectedLog := "reconciling WorkloadPolicy"
	found := false
	for _, logMsg := range sink.logs {
		if logMsg == expectedLog {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected log message %q was not recorded by the context logger. Recorded logs: %v", expectedLog, sink.logs)
	}
}
