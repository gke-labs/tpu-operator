package gce

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"testing"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/encoding/protojson"
	"k8s.io/utils/ptr"
)

func TestListManagedInstances(t *testing.T) {
	tests := []struct {
		name          string
		migName       string
		respInstances []*computepb.ManagedInstance
		wantErr       bool
	}{
		{
			name:    "Success",
			migName: "test-mig",
			respInstances: []*computepb.ManagedInstance{
				{Instance: ptr.To("inst-1"), CurrentAction: ptr.To("NONE")},
				{Instance: ptr.To("inst-2"), CurrentAction: ptr.To("CREATING")},
			},
		},
		{
			name:    "Error",
			migName: "test-mig",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wantErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				resp := &computepb.InstanceGroupManagersListManagedInstancesResponse{
					ManagedInstances: tc.respInstances,
				}
				w.Header().Set("Content-Type", "application/json")
				data, _ := protojson.Marshal(resp)
				w.Write(data)
				return
			}))
			defer server.Close()

			ctx := context.Background()
			igmClient, err := compute.NewInstanceGroupManagersRESTClient(ctx, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("Failed to create igm client: %v", err)
			}

			mgr := &Manager{igmClient: igmClient}

			instances, err := mgr.IGM().ListManagedInstances(ctx, "test-project", "test-zone", tc.migName)
			if (err != nil) != tc.wantErr {
				t.Errorf("ListManagedInstances() error = %v, wantErr %v", err, tc.wantErr)
			}

			if len(instances) != len(tc.respInstances) {
				t.Errorf("Expected %d instances, got %d", len(tc.respInstances), len(instances))
			}
		})
	}
}

// MockIterator is a mock implementation of managedInstanceIterator.
type MockIterator struct {
	items []*computepb.ManagedInstance
	index int
	err   error
}

func (m *MockIterator) Next() (*computepb.ManagedInstance, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.index >= len(m.items) {
		return nil, iterator.Done
	}
	item := m.items[m.index]
	m.index++
	return item, nil
}

func TestIterateInstances(t *testing.T) {
	tests := []struct {
		name          string
		mockIt        *MockIterator
		wantInstances []*computepb.ManagedInstance
		wantErr       bool
	}{
		{
			name: "Success",
			mockIt: &MockIterator{
				items: []*computepb.ManagedInstance{
					{Instance: ptr.To("inst-1")},
					{Instance: ptr.To("inst-2")},
				},
			},
			wantInstances: []*computepb.ManagedInstance{
				{Instance: ptr.To("inst-1")},
				{Instance: ptr.To("inst-2")},
			},
		},
		{
			name: "Error",
			mockIt: &MockIterator{
				err: fmt.Errorf("mock error"),
			},
			wantErr: true,
		},
		{
			name: "Empty",
			mockIt: &MockIterator{
				items: []*computepb.ManagedInstance{},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instances, err := iterateInstances(tc.mockIt)
			if (err != nil) != tc.wantErr {
				t.Errorf("iterateInstances() error = %v, wantErr %v", err, tc.wantErr)
			}

			if len(instances) != len(tc.wantInstances) {
				t.Errorf("Expected %d instances, got %d", len(tc.wantInstances), len(instances))
			}

			for i, inst := range instances {
				if inst.GetInstance() != tc.wantInstances[i].GetInstance() {
					t.Errorf("Expected instance %s, got %s", tc.wantInstances[i].GetInstance(), inst.GetInstance())
				}
			}
		})
	}
}

func TestInstanceClient_Get(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		respInstance *computepb.Instance
		wantErr      bool
	}{
		{
			name:         "Success",
			instanceName: "test-instance",
			respInstance: &computepb.Instance{
				Name: ptr.To("test-instance"),
				Metadata: &computepb.Metadata{
					Fingerprint: ptr.To("abc"),
				},
			},
		},
		{
			name:         "Error",
			instanceName: "test-instance",
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wantErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				data, _ := protojson.Marshal(tc.respInstance)
				w.Write(data)
			}))
			defer server.Close()

			ctx := context.Background()
			instancesClient, err := compute.NewInstancesRESTClient(ctx, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("Failed to create instances client: %v", err)
			}

			mgr := &Manager{instancesClient: instancesClient}

			req := &computepb.GetInstanceRequest{
				Project:  "test-project",
				Zone:     "test-zone",
				Instance: tc.instanceName,
			}
			inst, err := mgr.Instances().Get(ctx, req)
			if (err != nil) != tc.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tc.wantErr)
			}

			if !tc.wantErr && inst.GetName() != tc.instanceName {
				t.Errorf("Expected instance name %s, got %s", tc.instanceName, inst.GetName())
			}
		})
	}
}

func TestInstanceClient_SetMetadata(t *testing.T) {
	tests := []struct {
		name         string
		instanceName string
		metadata     *computepb.Metadata
		wantErr      bool
	}{
		{
			name:         "Success",
			instanceName: "test-instance",
			metadata: &computepb.Metadata{
				Fingerprint: ptr.To("abc"),
				Items: []*computepb.Items{
					{Key: ptr.To("key1"), Value: ptr.To("val1")},
				},
			},
		},
		{
			name:         "Error",
			instanceName: "test-instance",
			metadata:     &computepb.Metadata{},
			wantErr:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			updateCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.wantErr {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				updateCalled = true
				w.Header().Set("Content-Type", "application/json")
				status := computepb.Operation_DONE
				data, _ := protojson.Marshal(&computepb.Operation{Status: &status})
				w.Write(data)
			}))
			defer server.Close()

			ctx := context.Background()
			instancesClient, err := compute.NewInstancesRESTClient(ctx, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()))
			if err != nil {
				t.Fatalf("Failed to create instances client: %v", err)
			}

			mgr := &Manager{instancesClient: instancesClient}

			req := &computepb.SetMetadataInstanceRequest{
				Project:          "test-project",
				Zone:             "test-zone",
				Instance:         tc.instanceName,
				MetadataResource: tc.metadata,
			}
			_, err = mgr.Instances().SetMetadata(ctx, req)
			if (err != nil) != tc.wantErr {
				t.Errorf("SetMetadata() error = %v, wantErr %v", err, tc.wantErr)
			}

			if !tc.wantErr && !updateCalled {
				t.Errorf("Expected SetMetadata to be called")
			}
		})
	}
}

func TestNewManager(t *testing.T) {
	ctx := t.Context()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		mgr, err := NewManager(ctx, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()))
		if err != nil {
			t.Fatalf("NewManager() unexpected error: %v", err)
		}
		if mgr == nil {
			t.Fatalf("Expected non-nil Manager")
		}

		if err := mgr.Close(); err != nil {
			t.Errorf("mgr.Close() unexpected error: %v", err)
		}
	})

	t.Run("PartialFailureCleanup", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		var closedClients []string
		onClientCloseForTest := func(name string) {
			closedClients = append(closedClients, name)
		}

		initIGMSuccess := func(ctx context.Context, opts ...option.ClientOption) (*compute.InstanceGroupManagersClient, error) {
			return compute.NewInstanceGroupManagersRESTClient(ctx, append(opts, option.WithEndpoint(server.URL), option.WithHTTPClient(server.Client()))...)
		}

		initInstancesFailure := func(ctx context.Context, opts ...option.ClientOption) (*compute.InstancesClient, error) {
			return nil, fmt.Errorf("forced instances client failure")
		}

		initInstanceTemplatesDummy := func(ctx context.Context, opts ...option.ClientOption) (*compute.InstanceTemplatesClient, error) {
			return nil, fmt.Errorf("should not be called")
		}

		_, err := newManagerWithConstructors(
			ctx,
			initIGMSuccess,
			initInstancesFailure,
			initInstanceTemplatesDummy,
			onClientCloseForTest,
		)

		if err == nil {
			t.Fatalf("Expected error due to forced failure, got nil")
		}

		// EXPLICITLY VERIFY that the first client's Close() was called properly
		if len(closedClients) != 1 || closedClients[0] != "igm" {
			t.Fatalf("Expected ['igm'] to be closed, got %v", closedClients)
		}
	})
}
