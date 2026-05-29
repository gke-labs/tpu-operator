package e2e

import (
	"context"
	"log"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// waitForAllDeleted polls until the given list object is empty.
func waitForAllDeleted(ctx context.Context, c client.Client, listObj client.ObjectList, opts ...client.ListOption) error {
	return wait.PollUntilContextTimeout(ctx, 5*time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err := c.List(ctx, listObj, opts...); err != nil {
			return false, err
		}
		v := reflect.ValueOf(listObj).Elem().FieldByName("Items")
		if v.IsValid() && v.Len() > 0 {
			log.Printf("Waiting for %d objects of type %T to be deleted...", v.Len(), listObj)
			return false, nil
		}
		return true, nil
	})
}
