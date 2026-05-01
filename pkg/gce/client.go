package gce

import (
	"context"
	"fmt"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// IGMClient defines methods for interacting with Instance Group Managers.
type IGMClient interface {
	ListManagedInstances(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error)
}

// managedInstanceIterator defines the interface for iterating over managed instances.
type managedInstanceIterator interface {
	Next() (*computepb.ManagedInstance, error)
}

// InstanceClient defines methods for interacting with Instances.
type InstanceClient interface {
	Get(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error)
	SetMetadata(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error)
}

// Manager provides a centralized point to manage various GCE resources.
type Manager struct {
	igmClient       *compute.InstanceGroupManagersClient
	instancesClient *compute.InstancesClient
}

// NewManager creates and initializes the GCE clients.
func NewManager(ctx context.Context) (*Manager, error) {
	igmClient, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create InstanceGroupManagers client: %w", err)
	}
	instancesClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		igmClient.Close()
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}
	return &Manager{igmClient: igmClient, instancesClient: instancesClient}, nil
}

// IGM returns the IGMClient.
func (m *Manager) IGM() IGMClient {
	return &igmClientWrapper{client: m.igmClient}
}

// Instances returns the InstanceClient.
func (m *Manager) Instances() InstanceClient {
	return &instanceClientWrapper{client: m.instancesClient}
}

type igmClientWrapper struct {
	client *compute.InstanceGroupManagersClient
}

func (w *igmClientWrapper) ListManagedInstances(ctx context.Context, project, zone, migName string) ([]*computepb.ManagedInstance, error) {
	req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
		Project:              project,
		Zone:                 zone,
		InstanceGroupManager: migName,
	}
	it := w.client.ListManagedInstances(ctx, req)
	return iterateInstances(it)
}

func iterateInstances(it managedInstanceIterator) ([]*computepb.ManagedInstance, error) {
	var instances []*computepb.ManagedInstance
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to iterate managed instances: %w", err)
		}
		instances = append(instances, resp)
	}
	return instances, nil
}

type instanceClientWrapper struct {
	client *compute.InstancesClient
}

func (w *instanceClientWrapper) Get(ctx context.Context, req *computepb.GetInstanceRequest) (*computepb.Instance, error) {
	return w.client.Get(ctx, req)
}

func (w *instanceClientWrapper) SetMetadata(ctx context.Context, req *computepb.SetMetadataInstanceRequest) (*compute.Operation, error) {
	return w.client.SetMetadata(ctx, req)
}

// Close closes the underlying clients.
func (m *Manager) Close() error {
	var errs []error
	if err := m.igmClient.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.instancesClient.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing clients: %v", errs)
	}
	return nil
}
