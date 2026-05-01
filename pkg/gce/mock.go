package gce

import (
	"context"

	compute "cloud.google.com/go/compute/apiv1"
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

// MockInstanceClient is a mock implementation of the gce.InstanceClient.
type MockInstanceClient struct {
	GetFunc         func(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error)
	SetMetadataFunc func(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockInstanceClient) Get(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, req)
	}
	return nil, nil
}

// SetMetadata calls the mocked SetMetadataFunc.
func (m *MockInstanceClient) SetMetadata(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
	if m.SetMetadataFunc != nil {
		return m.SetMetadataFunc(ctx, req)
	}
	return nil, nil
}
