# CRD Design (`tpu.google.com/v1alpha1`)

## Overview

The user interacts with the TPU Node Group Controller primarily through the `TPUNodeGroup` Custom Resource. The `TPUNodeGroup` specifies the desired state of a TPU slice or capacity group. To provide granular visibility into large topologies without overloading a single status block on the `TPUNodeGroup`, the controller manages child `TPUNodeState` resources for each underlying VM.

**Scope:** Both `TPUNodeGroup` and `TPUNodeState` are **Cluster-scoped** resources (`scope: Cluster` in their CRD definitions). Because they provision and map directly to Kubernetes `Node` objects (which are inherently cluster-scoped) and create globally unique GCE infrastructure, a cluster scope prevents naming collisions that would occur if multiple namespaces attempted to provision TPU groups with the same metadata name.

**Project Structure:** The Go structural definitions for these Custom Resources are placed in a subfolder named `apis` in the project repository (e.g., `apis/v1alpha1/tpunodegroup_types.go`).

This document details the exact structure, validation rules, and status representations for these Custom Resources.

---

## `TPUNodeGroup` Spec

The `TPUNodeGroup` specification allows users to define the physical characteristics of the TPU hardware, the GCE configuration of the backing VMs, and optional bootstrapping settings.

### Fields

*   **`project`** (`string`, Required): The GCP project ID where the resources will be created.
*   **`location`** (`string`, Required): The GCE Zone (e.g., `us-central1-a`) where the VMs will be provisioned.

**Infrastructure Configuration (Mutually Exclusive)**
Users provide either `instanceTemplate` OR the discrete fields (`machineType`, `provisioningModel`, etc.).

*   **`instanceTemplate`** (`string`, Optional): Full URI to an existing GCE Instance Template.
*   **`machineType`** (`string`, Optional): The TPU machine type (e.g., `tpu7x-standard-4t`).
*   **`provisioningModel`** (`string`, Optional): The consumption model. Allowed values: `standard`, `spot`, `reservation-bound`.
*   **`reservation`** (`string`, Optional): The reservation name. Required if `provisioningModel` is `reservation-bound`.
*   **`image`** (`string`, Optional): The URI of the boot disk image. If omitted, the controller defaults to `projects/ml-images/global/images/family/tpu-ubuntu2204-base`.
*   **`bootDiskSize`** (`int32`, Optional): The size of the boot disk in GB.
*   **`subnetwork`** (`string`, Required): The URI of the GCP subnetwork (specifying only the subnetwork prevents configuration clashes with the parent network).
*   **`serviceAccount`** (`string`, Optional): The email of the GCP service account to attach to the VMs.

**Topology and Sizing Configuration**

*   **`acceleratorConnectionMode`** (`string`, Required): Allowed values: `static` (Phase 1 support only).
*   **`topology`** (`string`, Required): Defines the physical ICI shape (e.g., `4x4x4`).
*   **`nodeCount`** (`int32`, Required): The number of VMs to provision. Required for both single-host and multi-host slices.

**Bootstrapping Configuration**

*   **`bootstrapKubernetes`** (`boolean`, Optional): Whether the controller should manage node bootstrapping. Default: `false`.
*   **`version`** (`string`, Optional): The Kubernetes version to install if `bootstrapKubernetes` is `true` (e.g., `1.29.2`).

### Validation Rules (CEL)

To ensure rapid feedback to the user, the Kubernetes API server enforces the following rules via CEL (Common Expression Language) validation:

1.  **Name Length:** The `metadata.name` is limited to 40 characters or fewer. Because the controller generates GCE resources (like `tpunodegroup-{CRD_NAME}-mig`) based on this name, it fits within GCP's strict 63-character limit.
2.  **Immutability:** The fields `project`, `location`, `nodeCount`, `acceleratorConnectionMode`, and `topology` cannot be modified after creation.
3.  **Generation Compatibility:** `machineType` string validation ensures only `v6e` and `v7x` machine types are accepted.
4.  **Template Exclusivity:** The user provides `instanceTemplate` OR the discrete fields (`machineType`, `image`, etc.). If both or neither are provided, the request is rejected.
5.  **Reservation Requirement:** If `provisioningModel` equals `reservation-bound`, the `reservation` field is a non-empty string.

*(Note: Mathematical alignment between `nodeCount`, `topology`, and `machineType` is NOT validated by the webhook. The controller relies on the GCE API to reject invalid sizing or topology combinations during MIG/Workload Policy creation.)*

---

## `TPUNodeGroup` Status

The `TPUNodeGroup` status provides a high-level, aggregated view of the entire slice's health.

### Fields

*   **`nodes`** (`object`): Aggregated node summary counts.
    *   **`totalNodes`** (`int32`): The total desired number of nodes.
    *   **`readyNodes`** (`int32`): The number of nodes fully provisioned and ready.
    *   **`provisioningNodes`** (`int32`): The number of nodes currently being provisioned.
    *   **`failed`** (`int32`): The number of nodes in a failed state.
*   **`conditions`** (`list of objects`): High-level condition tracking.
    *   **`type`** (`string`): The condition type (e.g., `Ready`, `Failed`).
    *   **`status`** (`string`): The status of the condition (`True`, `False`, `Unknown`).
    *   **`reason`** (`string`): A machine-readable, CamelCase reason for the condition's last transition.
    *   **`message`** (`string`): A human-readable message indicating details about the transition.
    *   **`lastTransitionTime`** (`string`): Timestamp of when the condition last changed state.

### Supported Status Conditions & Reasons

The controller updates the `Ready` and `Failed` conditions based on the underlying progress of the GCE API and Kubernetes Nodes. A `Failed: True` state indicates a terminal or non-retryable error that requires explicit user intervention.

| Scenario / Controller Action | `Ready` Status | `Ready` Reason | `Failed` Status | `Failed` Reason | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Initial Creation** | `Unknown` | `Pending` | `Unknown` | `Pending` | The CR has just been accepted by the API server. |
| **Provisioning Infrastructure** | `False` | `GCEResourceCreating` | `False` | - | The controller is actively calling the GCE API to create Instance Templates, Workload Policies, or MIGs. |
| **Bootstrapping VMs** | `False` | `AwaitingNodes` | `False` | - | GCE resources are created, but the controller is waiting for VMs to boot, read tokens, and register as Kubernetes Nodes. |
| **Temporary API Issues** | `False` | `APIError` | `False` | - | The controller encountered transient network or GCE API errors (e.g., 503s). It will automatically retry. |
| **Node Health Issues** | `False` | `NodeNotReady` | `False` | - | A node joined the cluster, but the TPU Device Plugin reported the hardware as unhealthy. |
| **All Systems Operational** | `True` | `NodesReady` | `False` | - | All requested nodes are provisioned, joined, and reporting healthy TPU capacity. |
| **Teardown Initiated** | `False` | `Terminating` | `False` | - | The `deletionTimestamp` is set. The controller is deleting GCE resources and cleaning up nodes. |
| **(Terminal) Missing IAM** | `False` | `IAMPermissionDenied` | `True` | `IAMPermissionDenied` | The controller's service account lacks required GCP permissions (e.g., `compute.instances.insert`). |
| **(Terminal) Quota Limits** | `False` | `QuotaExceeded` | `True` | `QuotaExceeded` | GCE rejected the creation request due to insufficient GCP quota for TPUs, CPUs, or IPs. |
| **(Terminal) Invalid GCP Spec** | `False` | `InvalidSpec` | `True` | `InvalidSpec` | The provided `instanceTemplate` doesn't exist, or the `machineType`/`topology` combination is rejected by the GCE API. |
| **(Terminal) Unexpected VM Death** | `False` | `NodeTerminated` | `True` | `NodeTerminated` | An underlying GCE VM was unexpectedly terminated or preempted outside of normal lifecycle events. |

---

## `TPUNodeState` Spec & Status

For a multi-host slice spanning 64 VMs, surfacing the individual state of every VM in the `TPUNodeGroup` status block would cause severe bloat. Therefore, the controller creates a `TPUNodeState` CR for *every VM* in the Managed Instance Group (MIG).

This allows operators to natively query `kubectl get tpunodestate` to pinpoint exactly which VM is stuck in provisioning or failing to join the cluster.

**Naming Convention:** The metadata name strictly follows: `{TPUNodeGroup.name}-{GCE-Instance-Name}`.

### Spec Fields

Read-only configuration reflecting the physical GCE instance.

*   **`instanceName`** (`string`, Required): The name of the GCE VM instance.
*   **`gceInstanceId`** (`string`, Required): The unique ID of the GCE instance.

### Status Fields

Granular tracking of the specific VM's journey.

*   **`gceInstanceStatus`** (`string`): Mirrors GCE states. Allowed values: `PROVISIONING`, `STAGING`, `RUNNING`, `STOPPING`, `TERMINATED`.
*   **`kubernetesNodeName`** (`string`): Populated once the node joins the cluster.
*   **`kubernetesNodeReady`** (`boolean`): Reflects the 'Ready' condition of the underlying Kubernetes Node object.
*   **`tokenState`** (`string`): Tracks managed bootstrapping. Allowed values: `Pending`, `Injected`, `CleanedUp`.
*   **`lastGCEError`** (`string`): Captures isolated GCE errors specific to this VM (e.g., localized stockout).
*   **`conditions`** (`list of objects`): Individual condition tracking for this node (e.g., `Ready`, `NodeJoined`, `TokenInjected`, `Failed`).

### Lifecycle & Garbage Collection
Because `TPUNodeState` objects contain an `ownerReference` pointing to their parent `TPUNodeGroup`, they are automatically and safely garbage collected by the Kubernetes control plane when the `TPUNodeGroup` is successfully deleted and its finalizers are removed.