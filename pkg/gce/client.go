package gce

import (
	"context"
	"errors"
	"fmt"

	"net/http"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
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

// Operation defines methods for interacting with long-running operations.
type Operation interface {
	Done() bool
	Name() string
}

// InstanceTemplateClient defines methods for interacting with Instance Templates.
type InstanceTemplateClient interface {
	Get(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error)
	Insert(ctx context.Context, project string, template *computepb.InstanceTemplate) (Operation, error)
	Delete(ctx context.Context, project, name string) (Operation, error)
}

// GlobalOperationsClient defines methods for interacting with Global Operations.
type GlobalOperationsClient interface {
	Get(ctx context.Context, project, operation string) (*computepb.Operation, error)
}

// Manager provides a centralized point to manage various GCE resources.
type Manager struct {
	igmClient               *compute.InstanceGroupManagersClient
	instancesClient         *compute.InstancesClient
	instanceTemplatesClient *compute.InstanceTemplatesClient
	globalOperationsClient  *compute.GlobalOperationsClient
}

// NewManager creates and initializes the GCE clients.
func NewManager(ctx context.Context, opts ...option.ClientOption) (*Manager, error) {
	return newManagerWithConstructors(
		ctx,
		compute.NewInstanceGroupManagersRESTClient,
		compute.NewInstancesRESTClient,
		compute.NewInstanceTemplatesRESTClient,
		compute.NewGlobalOperationsRESTClient,
		nil, // No observation hook needed in production
		opts...,
	)
}

type newIGMClientFunc func(context.Context, ...option.ClientOption) (*compute.InstanceGroupManagersClient, error)
type newInstancesClientFunc func(context.Context, ...option.ClientOption) (*compute.InstancesClient, error)
type newInstanceTemplatesClientFunc func(context.Context, ...option.ClientOption) (*compute.InstanceTemplatesClient, error)
type newGlobalOperationsClientFunc func(context.Context, ...option.ClientOption) (*compute.GlobalOperationsClient, error)

// newManagerWithConstructors allows injecting dependencies internally for testing.
func newManagerWithConstructors(
	ctx context.Context,
	newIGMClient newIGMClientFunc,
	newInstancesClient newInstancesClientFunc,
	newInstanceTemplatesClient newInstanceTemplatesClientFunc,
	newGlobalOperationsClient newGlobalOperationsClientFunc,
	onClientClose func(clientName string),
	opts ...option.ClientOption,
) (mgr *Manager, err error) {
	var cleanups []func() error
	defer func() {
		if err != nil {
			for i := len(cleanups) - 1; i >= 0; i-- {
				_ = cleanups[i]()
			}
		}
	}()

	igmClient, err := newIGMClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create InstanceGroupManagers client: %w", err)
	}
	cleanups = append(cleanups, func() error {
		if onClientClose != nil {
			onClientClose("igm")
		}
		return igmClient.Close()
	})

	instancesClient, err := newInstancesClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}
	cleanups = append(cleanups, func() error {
		if onClientClose != nil {
			onClientClose("instances")
		}
		return instancesClient.Close()
	})

	instanceTemplatesClient, err := newInstanceTemplatesClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance templates client: %w", err)
	}
	cleanups = append(cleanups, func() error {
		if onClientClose != nil {
			onClientClose("instanceTemplates")
		}
		return instanceTemplatesClient.Close()
	})

	globalOperationsClient, err := newGlobalOperationsClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create global operations client: %w", err)
	}
	cleanups = append(cleanups, func() error {
		if onClientClose != nil {
			onClientClose("globalOperations")
		}
		return globalOperationsClient.Close()
	})

	return &Manager{
		igmClient:               igmClient,
		instancesClient:         instancesClient,
		instanceTemplatesClient: instanceTemplatesClient,
		globalOperationsClient:  globalOperationsClient,
	}, nil
}

// IGM returns the IGMClient.
func (m *Manager) IGM() IGMClient {
	return &igmClientWrapper{client: m.igmClient}
}

// Instances returns the InstanceClient.
func (m *Manager) Instances() InstanceClient {
	return &instanceClientWrapper{client: m.instancesClient}
}

// InstanceTemplates returns the InstanceTemplateClient.
func (m *Manager) InstanceTemplates() InstanceTemplateClient {
	return &instanceTemplateClientWrapper{client: m.instanceTemplatesClient}
}

// GlobalOperations returns the GlobalOperationsClient.
func (m *Manager) GlobalOperations() GlobalOperationsClient {
	return &globalOperationsClientWrapper{client: m.globalOperationsClient}
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

type instanceTemplateClientWrapper struct {
	client *compute.InstanceTemplatesClient
}

func (w *instanceTemplateClientWrapper) Get(ctx context.Context, project, name string) (*computepb.InstanceTemplate, error) {
	req := &computepb.GetInstanceTemplateRequest{
		Project:          project,
		InstanceTemplate: name,
	}
	return w.client.Get(ctx, req)
}

type operationWrapper struct {
	op *compute.Operation
}

func (w *operationWrapper) Done() bool {
	if w.op == nil {
		return true
	}
	return w.op.Done()
}

func (w *operationWrapper) Name() string {
	if w.op == nil {
		return ""
	}
	return w.op.Name()
}

func (w *instanceTemplateClientWrapper) Insert(ctx context.Context, project string, template *computepb.InstanceTemplate) (Operation, error) {
	req := &computepb.InsertInstanceTemplateRequest{
		Project:                  project,
		InstanceTemplateResource: template,
	}
	op, err := w.client.Insert(ctx, req)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, nil
	}
	return &operationWrapper{op: op}, nil
}

func (w *instanceTemplateClientWrapper) Delete(ctx context.Context, project, name string) (Operation, error) {
	req := &computepb.DeleteInstanceTemplateRequest{
		Project:          project,
		InstanceTemplate: name,
	}
	op, err := w.client.Delete(ctx, req)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, nil
	}
	return &operationWrapper{op: op}, nil
}

type globalOperationsClientWrapper struct {
	client *compute.GlobalOperationsClient
}

func (w *globalOperationsClientWrapper) Get(ctx context.Context, project, operation string) (*computepb.Operation, error) {
	req := &computepb.GetGlobalOperationRequest{
		Project:   project,
		Operation: operation,
	}
	return w.client.Get(ctx, req)
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
	if err := m.instanceTemplatesClient.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := m.globalOperationsClient.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors closing clients: %v", errs)
	}
	return nil
}

// IsNotFound returns true if the error is a GCE 404.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	var ae *googleapi.Error
	if errors.As(err, &ae) {
		return ae.Code == http.StatusNotFound
	}
	return false
}
