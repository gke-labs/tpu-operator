package gce

import (
	"context"

	computepb "cloud.google.com/go/compute/apiv1/computepb"
)

// MockIGMClient is a mock implementation of the gce.IGMClient.
type MockIGMClient struct {
	ListManagedInstancesFunc func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error)
}

// ListManagedInstances calls the mocked ListManagedInstancesFunc.
func (m *MockIGMClient) ListManagedInstances(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
	if m.ListManagedInstancesFunc != nil {
		return m.ListManagedInstancesFunc(ctx, project, zone, migName)
	}
	return nil, nil
}
