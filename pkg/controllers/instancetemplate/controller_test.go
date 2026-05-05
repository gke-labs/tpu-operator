package instancetemplate

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	tpuv1alpha1 "gke-internal.googlesource.com/tpu-node-group/pkg/apis/tpu/v1alpha1"
)

func TestInstanceTemplateReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = tpuv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name          string
		request       reconcile.Request
		initialObject *tpuv1alpha1.InstanceTemplate
		wantResult    reconcile.Result
		wantErr       bool
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
			name: "resource_found",
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
				Spec: tpuv1alpha1.InstanceTemplateSpec{},
			},
			wantResult: reconcile.Result{},
			wantErr:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			objs := []client.Object{}
			if tc.initialObject != nil {
				objs = append(objs, tc.initialObject)
			}

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
			r := &InstanceTemplateReconciler{
				Client:   cl,
				Scheme:   scheme,
				Recorder: record.NewFakeRecorder(10),
			}

			gotResult, err := r.Reconcile(context.Background(), tc.request)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("Reconcile(%v) = (%v, %v), want error presence = %v", tc.request, gotResult, err, tc.wantErr)
			}

			if diff := cmp.Diff(tc.wantResult, gotResult); diff != "" {
				t.Errorf("Reconcile(%v) result mismatch (-want +got):\n%s", tc.request, diff)
			}
		})
	}
}
