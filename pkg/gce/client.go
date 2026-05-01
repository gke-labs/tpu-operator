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

// Manager provides a centralized point to manage various GCE resources.
type Manager struct {
	igmClient *compute.InstanceGroupManagersClient
}

// NewManager creates and initializes the GCE clients.
func NewManager(ctx context.Context) (*Manager, error) {
	igmClient, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create InstanceGroupManagers client: %w", err)
	}
	return &Manager{igmClient: igmClient}, nil
}

// IGM returns the IGMClient.
func (m *Manager) IGM() IGMClient {
	return &igmClientWrapper{client: m.igmClient}
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

// Close closes the underlying clients.
func (m *Manager) Close() error {
	if err := m.igmClient.Close(); err != nil {
		return fmt.Errorf("failed to close igm client: %w", err)
	}
	return nil
}
