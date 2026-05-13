package managedinstancegroup

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
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

			r := &ManagedInstanceGroupReconciler{
				Client: cl,
				Scheme: scheme,
				Log:    logr.Discard(),
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
