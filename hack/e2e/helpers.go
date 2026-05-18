package e2e

import (
	"context"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func applyManifest(ctx context.Context, k8sClient client.Client, path string, obj client.Object) error {
	yamlBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(yamlBytes, obj); err != nil {
		return err
	}
	return k8sClient.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("e2e-test"))
}

func waitForCondition[T client.Object](
	ctx context.Context, k8sClient client.Client, key client.ObjectKey, obj T,
	getConditions func(T) []metav1.Condition, condType string, expectedStatus metav1.ConditionStatus, timeout time.Duration,
) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		if err := k8sClient.Get(ctx, key, obj); err != nil {
			return false, err
		}
		for _, c := range getConditions(obj) {
			if c.Type == condType && c.Status == expectedStatus {
				return true, nil
			}
		}
		return false, nil
	})
}

func waitForDeletion(ctx context.Context, k8sClient client.Client, key client.ObjectKey, obj client.Object, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		err := k8sClient.Get(ctx, key, obj)
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	})
}
