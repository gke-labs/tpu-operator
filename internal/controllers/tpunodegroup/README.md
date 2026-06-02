# TPUNodeGroup Controller

The primary goal of the `TPUNodeGroup` controller is to serve as the orchestrator in a Kubernetes composite controller pattern, translating user intent for TPU capacity into fully provisioned, bootstrapped, and usable Kubernetes nodes on Google Compute Engine (GCE).

## 🎯 Desired State (Post-CR Creation)

When a user submits a `TPUNodeGroup` Custom Resource (CR), the controller’s goal is to reach the following steady state:

1.  **Infrastructure Provisioned**: Underlying GCE resources—specifically a Workload Policy (for multi-host placement), an Instance Template, and a Managed Instance Group (MIG)—are provisioned in GCE matching the requested TPU topology (e.g., `v4-8`).
2.  **Atomic Slice Readiness**: For multi-host TPU slices, the MIG is provisioned using `bulk` allocation mode to ensure all VMs backing the slice are created atomically.
3.  **Cluster Membership**: All GCE VMs backing the MIG have successfully booted, received bootstrapping credentials, and joined the Kubernetes cluster as registered `Node` objects.
4.  **Hardware Verification**: The TPU Device Plugin running on the nodes has verified Inter-Chip Interconnect (ICI) health and applied appropriate TPU capacity labels.
5.  **CR Status Reflected**: 
    *   `TPUNodeGroup` conditions show `Ready: True`, `Reconciling: False`, and `Failed: False`.
    *   The `nodes` status summary shows `readyNodes` equal to `totalNodes`.

## 🚀 Step-by-Step Workflow

To achieve this desired state, the controller executes a structured lifecycle reconciliation process divided into distinct phases:

### Phase 1: Intent & Child Resource Orchestration (Upper Layer)
The controller processes the `TPUNodeGroup` spec and orchestrates the creation of child Custom Resources in a strict dependency order:
1.  **Create `WorkloadPolicy` CR**: Created first if the topology requires a placement policy (essential for multi-host TPU slices to ensure optimal network proximity).
2.  **Create `InstanceTemplate` CR**: Defines machine type, OS image, and base metadata.
3.  **Create `ManagedInstanceGroup` CR**: Created only after the template and policy are ready. It references both and enforces TPU-specific flags (e.g., `--target-size-policy-mode=bulk` and `--default-action-on-vm-failure=do-nothing`).

### Phase 2: Infrastructure Provisioning (Lower Layer Delegation)
*   The `TPUNodeGroup` controller pauses progress while monitoring the status of its child CRs.
*   Lower-layer custom controllers reconcile the child CRs, making the imperative GCE API calls to create the actual cloud infrastructure.
*   The upper controller bubbles up child conditions (e.g., `InstanceTemplateReady`, `MIGReady`) into the `TPUNodeGroup` status for user visibility.

### Phase 3: Bootstrapping & Node Join
Once the child MIG reports that GCE VMs are in a `RUNNING` state:
1.  **Metadata & Token Injection**: The controller generates a cluster join token and computes required TPU labels (accelerator type, count, topology), injecting them into the GCE VM metadata.
2.  **Node Matching & Labeling**: The controller monitors the cluster for joining nodes, matches them to GCE instances using the node's `ProviderID` (constructed as `gce://<project>/<zone>/<instance-name>`), and ensures they are labeled with appropriate TPU labels in Kubernetes.
3.  **Awaiting Readiness**: The controller waits for Kubernetes `Node` objects to register and for the TPU Device Plugin to mark the nodes healthy.
4.  **Finalize Ready State**: Once all nodes are ready (or `minReadyNodes` is met for single-host topologies), the controller sets the `Ready` condition to `True`.

### Phase 4: Graceful Teardown (On Deletion)
When a `TPUNodeGroup` CR is marked for deletion, the controller executes a safe teardown sequence before releasing its finalizer:
1.  **Cordon**: Cordon the corresponding Kubernetes nodes (adds `NoSchedule` taint) to prevent new pods from being scheduled.
2.  **Reverse Teardown**: Delete child CRs in reverse dependency order (`ManagedInstanceGroup` -> `InstanceTemplate` -> `WorkloadPolicy`).
3.  **Finalizer Removal**: Once child controllers confirm all GCE resources are destroyed, the `TPUNodeGroup` controller removes its finalizer, allowing the CR to be garbage collected.
