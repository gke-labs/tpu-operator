package gce

import (
	"context"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
)

// MockIGMClient is a mock implementation of the gce.IGMClient.
type MockIGMClient struct {
	ListManagedInstancesFunc func(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error)
	GetFunc    func(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error)
	InsertFunc func(ctx context.Context, project, zone string, igm *computepb.InstanceGroupManager) (Operation, error)
	DeleteFunc func(ctx context.Context, project, zone, name string) (Operation, error)
}

// ListManagedInstances calls the mocked ListManagedInstancesFunc.
func (m *MockIGMClient) ListManagedInstances(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
	if m.ListManagedInstancesFunc != nil {
		return m.ListManagedInstancesFunc(ctx, project, zone, migName)
	}
	return nil, nil
}

// Get calls the mocked GetFunc.
func (m *MockIGMClient) Get(ctx context.Context, project, zone, name string) (*computepb.InstanceGroupManager, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, zone, name)
	}
	return nil, nil
}

// Insert calls the mocked InsertFunc.
func (m *MockIGMClient) Insert(ctx context.Context, project, zone string, igm *computepb.InstanceGroupManager) (Operation, error) {
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, project, zone, igm)
	}
	return nil, nil
}

// Delete calls the mocked DeleteFunc.
func (m *MockIGMClient) Delete(ctx context.Context, project, zone, name string) (Operation, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, project, zone, name)
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

// MockOperation is a mock implementation of gce.Operation.
type MockOperation struct {
	DoneFunc func() bool
	NameFunc func() string
}

func (m *MockOperation) Done() bool {
	if m.DoneFunc != nil {
		return m.DoneFunc()
	}
	return true
}

func (m *MockOperation) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock-operation"
}

// MockInstanceTemplateClient is a mock implementation of the gce.InstanceTemplateClient.
type MockInstanceTemplateClient struct {
	GetFunc    func(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error)
	InsertFunc func(ctx context.Context, project string, template *computepb.InstanceTemplate) (Operation, error)
	DeleteFunc func(ctx context.Context, project, name string) (Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockInstanceTemplateClient) Get(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, name)
	}
	return nil, nil
}

// Insert calls the mocked InsertFunc.
func (m *MockInstanceTemplateClient) Insert(ctx context.Context, project string, template *computepb.InstanceTemplate) (Operation, error) {
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, project, template)
	}
	return nil, nil
}

// Delete calls the mocked DeleteFunc.
func (m *MockInstanceTemplateClient) Delete(ctx context.Context, project, name string) (Operation, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, project, name)
	}
	return nil, nil
}

// MockGlobalOperationsClient is a mock implementation of the gce.GlobalOperationsClient.
type MockGlobalOperationsClient struct {
	GetFunc func(ctx context.Context, project, operation string) (*computepb.Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockGlobalOperationsClient) Get(ctx context.Context, project, operation string) (*computepb.Operation, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, operation)
	}
	return nil, nil
}

// MockResourcePolicyClient is a mock implementation of the gce.ResourcePolicyClient.
type MockResourcePolicyClient struct {
	GetFunc    func(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error)
	InsertFunc func(ctx context.Context, project, region string, policy *computepb.ResourcePolicy) (Operation, error)
	DeleteFunc func(ctx context.Context, project, region, name string) (Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockResourcePolicyClient) Get(ctx context.Context, project, region, name string) (*computepb.ResourcePolicy, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, region, name)
	}
	return nil, nil
}

// Insert calls the mocked InsertFunc.
func (m *MockResourcePolicyClient) Insert(ctx context.Context, project, region string, policy *computepb.ResourcePolicy) (Operation, error) {
	if m.InsertFunc != nil {
		return m.InsertFunc(ctx, project, region, policy)
	}
	return nil, nil
}

// Delete calls the mocked DeleteFunc.
func (m *MockResourcePolicyClient) Delete(ctx context.Context, project, region, name string) (Operation, error) {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, project, region, name)
	}
	return nil, nil
}

// MockRegionOperationsClient is a mock implementation of the gce.RegionOperationsClient.
type MockRegionOperationsClient struct {
	GetFunc func(ctx context.Context, project, region, operation string) (*computepb.Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockRegionOperationsClient) Get(ctx context.Context, project, region, operation string) (*computepb.Operation, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, region, operation)
	}
	return nil, nil
}

// MockZoneOperationsClient is a mock implementation of the gce.ZoneOperationsClient.
type MockZoneOperationsClient struct {
	GetFunc func(ctx context.Context, project, zone, operation string) (*computepb.Operation, error)
}

// Get calls the mocked GetFunc.
func (m *MockZoneOperationsClient) Get(ctx context.Context, project, zone, operation string) (*computepb.Operation, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, project, zone, operation)
	}
	return nil, nil
}
