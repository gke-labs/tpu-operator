# Implementation Plan - Node Bootstrapping (Full)

This plan outlines the full implementation of Node Bootstrapping for the TPU Node Group Controller, focusing on the scenario where bootstrapping is enabled (`bootstrapKubernetes.enabled: true`).

We will implement this in parts to keep changes small and reviewable, but this plan covers the entire scope.

## User Review Required

> [!IMPORTANT]
> - We are using the GCE instance name for node lookup in Part 1, with a TODO to transition to `providerID` later.
> - For testing in Kind, we will mock GCE API responses to simulate VM states and metadata updates.

> [!NOTE]
> The entire bootstrapping script (including token polling and running `kubeadm join`) will be included in the startup script of the MIG (not the instance template, and not in the base image). This plan focuses on the controller-side logic to generate and inject the required metadata.

## Open Questions

- None at this moment.

## Controller Responsibilities

- **MIG Reconciler**: Compares GCE managed instances with Kubernetes nodes, and populates `total` and `ready` counts in the status. "Ready" here means that there is a matching K8s `Node` object for the GCE instance.
- **Main Controller**: Reads the counts in the status of MIG, and surfaces the information via a condition.

## Proposed Changes

### Part 1: Node Watching and Status Tracking

Focuses on detecting when nodes join the cluster and updating the `TPUNodeGroup` status.

#### [NEW] [bootstrapper.go](../pkg/controllers/tpunodegroup/bootstrapper.go)

- Create a `NodeBootstrapper` struct that takes `client.Client` (K8s client) and `gce.IGMClient`.
- Implement `ReconcileNodeJoin(ctx context.Context, group *tpuapi.TPUNodeGroup) error`.
- This function will:
    1.  Use `igmClient.ListManagedInstances` to get the list of expected instances for the MIG.
    2.  List `Node` objects in the cluster.
    3.  Match Nodes to expected instances by comparing the Node name with the GCE instance name (leave a TODO to use `providerID` as the lookup).
    4.  Update the `TPUNodeGroup` status `NodeSummary` with the counts of total, ready, and pending nodes.

### Part 2: Token Generation and Metadata Injection

Focuses on generating `kubeadm` tokens and injecting them into GCE VM metadata.

#### Update [bootstrapper.go](file:///usr/local/google/home/hodamo/git/tpu-node-group/pkg/controllers/tpunodegroup/bootstrapper.go)

- Add method `InjectJoinTokens(ctx context.Context, group *tpuapi.TPUNodeGroup) error`.
- This function will:
    1.  Use `igmClient.ListManagedInstances` to find instances in `RUNNING` state.
    2.  For each running instance, check if a token has already been injected (by checking GCE metadata via `instanceClient.Get`).
    3.  If not injected:
        - Generate a short-lived `kubeadm` join token by creating a K8s Secret of type `bootstrap.kubernetes.io/token` in the `kube-system` namespace (this avoids needing the `kubeadm` CLI).
        - Retrieve the Control Plane IP from the `TPUNodeGroup` CR field (`spec.bootstrapKubernetes.controlPlaneIP`).
        - Compute the CA hash by reading the cluster's CA certificate and calculating the SHA-256 hash of its public key.
        - Call `instanceClient.SetMetadata` to inject the token, IP, and hash into the VM metadata.

### Part 3: Integration and Cleanup

Integrates all parts and handles cleanup.

#### Update [controller.go](file:///usr/local/google/home/hodamo/git/tpu-node-group/pkg/controllers/tpunodegroup/controller.go)

- Update `reconcileNodeBootstrapping` to:
    1.  Call `InjectJoinTokens` to ensure running VMs get tokens.
    2.  Call `ReconcileNodeJoin` to update status based on joined nodes.
    3.  Implement token cleanup: After a node successfully joins (detected in Part 1), delete the corresponding K8s bootstrap token secret to minimize exposure.

## Verification Plan

### Automated Tests
- **Part 1**: Unit test with fake K8s client and mock `IGMClient` returning instances. Verify status updates by creating fake `Node` objects with names matching the expected GCE instance names.
- **Part 2**: Unit test with mock `InstanceClient` to verify `SetMetadata` calls. Verify K8s Secret creation with fake K8s client.
- **Part 3**: Integration test simulating the full flow with mocks.

### Manual Verification
- Verify in a Kind cluster by manually applying resources and simulating VM state changes if possible.

## TODO
- TODO(b/500810349): Verify the full flow e2e on a real GKE/GCE cluster with real GCP services.
