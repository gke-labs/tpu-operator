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
			wantErr:       true,
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


