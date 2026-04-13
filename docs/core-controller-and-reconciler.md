# Core Controller & Reconciler Logic

## Overview

The Core Controller is the primary operator responsible for managing the lifecycle of `TPUNodeGroup` resources. It runs a level-triggered reconciliation loop to ensure the actual state of the cluster and GCE infrastructure matches the desired state declared by the user.

This document details the exact reconciliation loop architecture, state machine transitions, event watches, and RBAC requirements needed by an engineer to implement the controller.

---

## Reconciler Architecture

The reconciler follows standard Kubernetes controller patterns (e.g., using controller-runtime/Kubebuilder). It is fundamentally idempotent and edge-triggered, meaning it can be safely interrupted and restarted at any time.

### Event Watches (Triggers)

The reconciliation loop is triggered by changes to the following objects:

1.  **`TPUNodeGroup` (Primary Resource):** Watch for Create, Update, and Delete events.
2.  **`TPUNodeState` (Owned Resource):** Watch for changes to child `TPUNodeState` objects. Use an `EnqueueRequestForOwner` event handler to trigger a reconciliation of the parent `TPUNodeGroup` when a child state changes (e.g., a token is injected or a node fails).
3.  **`Node` (External Resource):** Watch for Kubernetes `Node` objects. The controller monitors when a newly bootstrapped VM successfully joins the cluster to update the corresponding `TPUNodeState.kubernetesNodeReady` status. Filter these watches using the label selector applied by the TPU Device Plugin (e.g., `cloud.google.com/gk8s-tpu-accelerator`).

### Error Handling & Backoff

*   **Transient Errors:** For temporary failures (e.g., GCE API 503s, network timeouts), the controller returns an error to trigger the default exponential backoff queue.
*   **Terminal Errors:** For irrecoverable errors (e.g., missing IAM permissions, Quota Exceeded, Invalid Machine Type), the controller updates the `Failed` status condition to `True`, emits a Kubernetes Event, and returns `Result{Requeue: false}` to avoid spamming the GCE API. It only re-reconciles if the `TPUNodeGroup` spec is updated.

---

## State Machine & Transitions

The reconciliation loop evaluates the current state and idempotently drives it toward the next state.

### 1. Initialization & Finalizer
*   **Action:** Verify the resource is a Cluster-scoped `TPUNodeGroup`. If it does not have the `tpu.google.com/slice-cleanup` finalizer, add it and update the object. Return immediately to re-trigger.
*   **Condition:** `Ready: Unknown`, `Reason: Pending`.

### 2. Validating
*   **Action:** Perform runtime validation that CEL could not catch.
    *   If `instanceTemplate` is provided, call the GCE API to ensure it exists and has `maintenance-policy=TERMINATE`.
    *   Check for required Workload Identity IAM permissions.
*   **Transition:** If valid, proceed to Creating Infrastructure. If invalid (terminal), set `Failed: True`, `Reason: InvalidSpec` or `IAMPermissionDenied`.

### 3. Creating Infrastructure
*   **Action:**
    *   For multi-host: Create the GCE `WORKLOAD` Resource Policy if it doesn't exist.
    *   Create the Managed Instance Group (MIG). For multi-host, use `target-size-policy-mode=bulk` and attach the Workload Policy.
*   **Condition:** Set `Ready: False`, `Reason: GCEResourceCreating`.
*   **Transition:** Once the GCE API confirms the MIG is created and scaling up, proceed to Awaiting Nodes.

### 4. Awaiting Nodes & TPUNodeState Management
*   **Action:**
    *   Call the GCE API to list instances in the MIG.
    *   **Sync `TPUNodeState` CRs:** Compare the list of GCE instances to the existing `TPUNodeState` objects. Create new CRs for new VMs. Delete CRs for VMs that no longer exist.
    *   Update the status of each `TPUNodeState` based on the GCE instance status (`PROVISIONING`, `STAGING`, `RUNNING`).
    *   If `bootstrapKubernetes: true`, handle token injection for `RUNNING` instances (detailed in the Bootstrapping doc).
*   **Condition:** Set `Ready: False`, `Reason: AwaitingNodes`.
*   **Transition:** Proceed to Ready only when all expected VMs are `RUNNING`, have joined the cluster as `Node` objects, and the `Ready` condition on all Kubernetes `Node` objects is `True`.

### 5. Ready
*   **Action:** Continuous monitoring. The controller periodically polls the GCE API (or relies on GCE Pub/Sub events if configured) to ensure the MIG size matches the desired count and no VMs have been unexpectedly terminated.
*   **Condition:** Set `Ready: True`, `Reason: NodesReady`.
*   **Transition:** If a VM fails or is deleted, transition back to Awaiting Nodes. If a `deletionTimestamp` is detected, transition to Terminating.

---

## Graceful Teardown (Deletion) Workflow

When the API server sets a `deletionTimestamp`, the controller intercepts the deletion via the finalizer and executes a strict two-phase cleanup.

**Pre-requisite Check:** Ensure the `deletionTimestamp` is not zero.

### Phase 1: Infrastructure Removal (GCE Cleanup)
*   **Action:**
    1.  Call GCE API to delete the Managed Instance Group (MIG).
    2.  Call GCE API to delete the associated Workload Policy (if applicable).
    *   *Note:* Do not delete user-provided Instance Templates.
*   **Condition:** Set `Ready: False`, `Reason: Terminating`, `Message: "Deleting GCE resources"`.
*   **Wait:** Requeue with a short delay (e.g., 10 seconds) and repeatedly check the GCE API. Do NOT proceed to Phase 2 until GCE returns `404 Not Found` for both the MIG and the Workload Policy.

### Phase 2: Kubernetes Cleanup & Finalization
*   **Action:**
    1.  Delete the associated Kubernetes `Node` objects from the cluster (to prevent them from sitting in a `NotReady` state indefinitely).
    2.  Remove the `tpu.google.com/slice-cleanup` finalizer from the `TPUNodeGroup` object.
    3.  Update the object in the Kubernetes API.
*   **Result:** The API server will completely remove the `TPUNodeGroup`, which triggers garbage collection of all owned `TPUNodeState` objects.

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