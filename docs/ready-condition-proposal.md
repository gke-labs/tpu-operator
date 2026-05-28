# Proposal: Single Ready Condition for TPU Node Group

**Author:** Antigravity
**Status:** Draft
**Date:** 2026-05-26

## Objective
To improve the visibility of the `TPUNodeGroup` resource status by adding a single top-level `Ready` condition that summarizes the progress of the cluster lifecycle, including node joining, labeling, and device plugin installation.

## Background
Currently, the `TPUNodeGroup` controller sets child-specific conditions (`WorkloadPolicyReady`, `InstanceTemplateReady`, `ManagedInstanceGroupReady`). It does not have a top-level `Ready` condition, nor does it track the state of nodes joining or device plugin installation via conditions.

## Proposed Changes

### 1. Top-Level `Ready` Condition
We will add a condition of type `Ready` to the `TPUNodeGroupStatus`. This condition will reflect the overall state of the resource and will use descriptive `Reason` and `Message` fields to indicate what step the controller is currently waiting on.

### 2. Evolving Reasons for `Ready` Condition
As the controller executes sequentially, the `Ready` condition will be updated with the following reasons:

*   **`AwaitingNodeJoin`**: Set when waiting for GCE VMs to join the Kubernetes cluster as nodes.
    *   Status: `False`
    *   Message: "Waiting for X of Y nodes to join the cluster"
*   **`Ready`**: Set when all steps are successful.
    *   Status: `True`
    *   Message: "All nodes are ready"

### 3. Deletion Reasons
During deletion (when `DeletionTimestamp` is not zero), the `Ready` condition will be set to `Status: False` with the following reasons as finalizers are processed:

*   **`DeletingMIG`**: Set when waiting for the child ManagedInstanceGroup to be deleted.
    *   Status: `False`
    *   Message: "Waiting for ManagedInstanceGroup to be deleted"
*   **`DeletingTemplate`**: Set when waiting for the child InstanceTemplate to be deleted.
    *   Status: `False`
    *   Message: "Waiting for InstanceTemplate to be deleted"
*   **`DeletingPolicy`**: Set when waiting for the child WorkloadPolicy to be deleted.
    *   Status: `False`
    *   Message: "Waiting for WorkloadPolicy to be deleted"
*   **`DeletingNodes`**: Set when deleting stale Kubernetes Node objects.
    *   Status: `False`
    *   Message: "Deleting stale Node objects"

### 4. Failure Reasons
When an error occurs during reconciliation, the `Ready` condition will be set to `Status: False` with the following reason:

*   **`ReconcileError`**: Set when any step fails with an error.
    *   Status: `False`
    *   Message: "Error reconciling: <error-message>"

### 5. Implementation Details

#### API Definitions
Add constants for the new condition types and reasons in `internal/apis/tpu/v1alpha1/tpunodegroup_types.go`.

#### Controller Logic
*   Update `ReconcileNodes` in `internal/controllers/tpunodegroup/node.go` to update the `Ready` condition with `Reason: AwaitingNodeJoin` or `LabelingNodes`.
*   Update `Reconcile` in `internal/controllers/tpunodegroup/controller.go` to check device plugin status and set `Ready` condition with `Reason: AwaitingDevicePlugin` or `Ready`.
*   Update `handleDeletion` in `internal/controllers/tpunodegroup/deletion.go` to update the `Ready` condition with deletion reasons.

## Alternatives Considered
*   **Adding multiple new conditions** (`NodesReady`, `DevicePluginReady`): Rejected in favor of a cleaner status with a single `Ready` condition and descriptive reasons.

## Verification Plan
*   Add unit tests in `controller_test.go` and `node_test.go` to verify condition updates.
