# Core Controller & Reconciler Logic

## Overview

The Core Controller is the primary operator responsible for managing the lifecycle of `TPUNodeGroup` resources. It runs a level-triggered reconciliation loop to ensure the actual state of the cluster and GCE infrastructure matches the desired state declared by the user.

This document details the exact reconciliation loop architecture, state machine transitions, event watches, and RBAC requirements needed to implement the controller.

---

## Reconciler Architecture

The reconciler follows standard Kubernetes controller patterns. It is fundamentally idempotent and edge-triggered, meaning it can be safely interrupted and restarted at any time.

### Event Watches (Triggers)

The reconciliation loop is triggered by changes to the following objects:

1.  **`TPUNodeGroup` (Primary Resource):** Watch for Create, and Delete events. Update events are not allowed.
3.  **`Node` (External Resource):** Watch for Kubernetes `Node` objects. The controller monitors when a newly bootstrapped VM successfully joins the cluster to update the corresponding `TPUNodeState.kubernetesNodeReady` status. Filter these watches using the label selector applied by the TPU Device Plugin (e.g., `cloud.google.com/gk8s-tpu-accelerator`).

### Error Handling & Backoff

*   **Transient Errors:** For temporary failures (e.g., GCE API 503s, network timeouts), the controller returns an error to trigger the default exponential backoff queue.
*   **Terminal Errors:** For irrecoverable errors (e.g., missing IAM permissions, Quota Exceeded, Invalid Machine Type), the controller updates the `Failed` status condition to `True`, emits a Kubernetes Event, and returns `Result{Requeue: false}` to avoid spamming the GCE API. It only re-reconciles if the `TPUNodeGroup` spec is updated.

---

## State Machine & Transitions

The reconciliation loop evaluates the current state and idempotently drives it toward the next state.

### 1. Initialization & Finalizer
*   **Action:** Verify the resource is a Cluster-scoped `TPUNodeGroup`. If it does not have the `tpu.google.com/slice-cleanup` finalizer, add it and update the object.
*   **Condition:** `Ready: Unknown`, `Failed: False`, `Reconciling: True`, `Reason: Initializing`.

### 2. Validating
*   **Action:** Perform runtime validation that CEL could not catch.
    *   If `instanceTemplate` is provided, call the GCE API to ensure it exists.
    *   Check for required Workload Identity IAM permissions.
*   **Transition:** If valid, proceed to Creating Infrastructure. If invalid (terminal), set `Failed: True`, `Reconciling: False`, `Reason: InvalidSpec` or `IAMPermissionDenied`.

### 3. Creating Infrastructure
*   **Action:**
    *   For multi-host: Create the GCE `WORKLOAD` Resource Policy if it doesn't exist.
    *   Create the Managed Instance Group (MIG). For multi-host, use `target-size-policy-mode=bulk` and attach the Workload Policy.
*   **Condition:** Set `Ready: False`, `Failed: False`, `Reconciling: True`, `Reason: GCEResourceCreating`.
*   **Transition:** Once the GCE API confirms the MIG is created and scaling up, proceed to Awaiting Nodes.

### 4. Awaiting Nodes Management
*   **Action:**
    *   Call the GCE API to list instances in the MIG.
    *   Create an internal in-memory map of each VM.
    *   If `spec.bootstrapKubernetes.enabled == true`, handle token injection for `RUNNING` instances (detailed in the Bootstrapping doc).
*   **Condition:** Set `Ready: False`, `Failed: False`, `Reconciling: True`, `Reason: AwaitingCapacity` or `AwaitingNodeJoin`.
*   **Transition:**
    *   For **single-host**: Proceed to `Ready: True` (with `Reason: MinNodesReached`) once `readyNodes >= spec.minNodeCount`.
    *   For **multi-host**: Proceed to `Ready: True` (with `Reason: FullyProvisioned`) only when *all* nodes are ready and the TPU slice is fully formed.

### 5. Ready
*   **Action:** Continuous monitoring. The controller periodically polls the GCE API to ensure the MIG size matches the desired count and no VMs have been unexpectedly terminated.
*   **Condition:** Set `Ready: True`, `Failed: False`, `Reconciling: False` (if fully provisioned), `Reason: FullyProvisioned` or `NodesReady`.
*   **Transition:** If a VM fails or is deleted, transition back to Awaiting Nodes (setting `Reconciling: True`). If a `deletionTimestamp` is detected, transition to Terminating.

---

## Graceful Teardown (Deletion) Workflow

When the API server sets a `deletionTimestamp`, the controller intercepts the deletion via the finalizer and executes a strict three-phase cleanup.

**Pre-requisite Check:** Ensure the `deletionTimestamp` is not zero.

### Phase 1: Cordon
*   **Action:** The controller marks nodes as Unschedulable by applying the `node.kubernetes.io/unschedulable: "NoSchedule"` taint to all Kubernetes Node objects managed by the CRD.
*   **Condition:** Set `Ready: False`, `Failed: False`, `Reconciling: True`, `Reason: CordoningNodes`.

### Phase 2: Infrastructure Removal (GCE Cleanup)
*   **Action:**
    1.  Call GCE API to delete the Managed Instance Group (MIG).
    2.  Call GCE API to delete the associated Workload Policy (if applicable).
    *   *Note:* Do not delete user-provided Instance Templates.
*   **Condition:** Set `Ready: False`, `Failed: False`, `Reconciling: True`, `Reason: DeletingGCE`, `Message: "Deleting GCE resources"`.
*   **Wait:** Requeue with a short delay and repeatedly check the GCE API. Do NOT proceed to Phase 3 until GCE returns `404 Not Found` for the MIG and Workload Policy.

### Phase 3: Kubernetes Cleanup & Finalization
*   **Action:**
    1.  Delete the associated Kubernetes `Node` objects from the cluster.
    2.  Remove the `tpu.google.com/slice-cleanup` finalizer from the `TPUNodeGroup` object.
*   **Condition:** Set `Ready: False`, `Failed: False`, `Reconciling: True`, `Reason: DeletingNodes`, `Message: "Deleting Kubernetes nodes"`.
*   **Result:** The API server will completely remove the `TPUNodeGroup`.

---

## Kubernetes RBAC Requirements

To execute this logic, the controller's ServiceAccount requires the following Kubernetes RBAC permissions:

| API Group | Resource | Verbs | Reason |
| :--- | :--- | :--- | :--- |
| `tpu.google.com` | `tpunodegroups` | `get, list, watch, update, patch, delete` | Primary resource management. |
| `tpu.google.com` | `tpunodegroups/status` | `get, update, patch` | Updating status conditions and node summaries. |
| `tpu.google.com` | `tpunodegroups/finalizers` | `update` | Managing the cleanup finalizer. |
| `tpu.google.com` | `tpunodestates` | `get, list, watch, create, update, patch, delete` | Managing child state objects. |
| `tpu.google.com` | `tpunodestates/status` | `get, update, patch` | Updating specific VM states. |
| `core` | `nodes` | `get, list, watch, delete` | Monitoring node readiness and cleaning up stale nodes during teardown. |
| `core` | `events` | `create, patch` | Emitting lifecycle events (e.g., `NodeJoined`, `GCEQuotaExceeded`). |