# CRD Design (`tpu.google.com/v1alpha1`)

## Overview

The user interacts with the TPU Node Group Controller primarily through the `TPUNodeGroup` Custom Resource. The `TPUNodeGroup` specifies the desired state of a TPU slice or capacity group.

**Scope:** Both `TPUNodeGroup` is a **Cluster-scoped** resources (`scope: Cluster` in their CRD definitions). Because they provision and map directly to Kubernetes `Node` objects (which are inherently cluster-scoped) and create globally unique GCE infrastructure, a cluster scope prevents naming collisions that would occur if multiple namespaces attempted to provision TPU groups with the same metadata name.

**Project Structure:** The Go structural definitions for these Custom Resources are placed in a subfolder named `apis` in the project repository (e.g., `apis/v1alpha1/tpunodegroup_types.go`).

This document details the exact structure, validation rules, and status representations for these Custom Resources.

---

### `TPUNodeGroup` Spec

The `TPUNodeGroup` specification allows users to define the physical characteristics of the TPU hardware, the GCE configuration of the backing VMs, and optional bootstrapping settings.

### Fields

*   **`project`** (`string`, Required): The GCP project ID where the resources will be created.
*   **`nodeLocation`** (`string`, Required): The GCE Zone (e.g., `us-central1-a`) where the VMs will be provisioned.
*   **`instanceTemplate`** (`*string`, Optional): Full URI to an existing GCE Instance Template. Cannot be set if `instanceConfig` is provided.
*   **`instanceConfig`** (`*InstanceConfig`, Optional): Allows the controller to generate an instance template. Cannot be set if `instanceTemplate` is provided.
*   **`nodeCount`** (`int32`, Required): The total number of VMs desired.
*   **`minNodeCount`** (`int32`, Optional): The minimum required for a single-host slice.
*   **`acceleratorConnectionMode`** (`string`, Required): Dictates how the chips are interconnected. Valid values are `static` or `dynamic`. (Immutable).
*   **`topology`** (`*string`, Optional): Specifies the physical arrangement of the TPU chips. Required for multi-host slices. If omitted, assumes single-host.
*   **`provisioningTimeoutMinutes`** (`*int32`, Optional): The reconciler times out waiting for the provisioning to be done and will update the status to Failed.
*   **`bootstrapKubernetes`** (`*BootstrapConfig`, Optional): Defines if and how the controller should install K8s components.

#### `InstanceConfig` Fields

*   **`machineType`** (`string`, Required): The GCE machine type (e.g., `tpu7x-standard-4t`).
*   **`provisioningModel`** (`*string`, Optional): Specifies spot, reservation-bound. Defaults to on-demand if not specified.
*   **`reservation`** (`*string`, Optional): The name of the reservation to consume. Required if `provisioningModel` is `reservation-bound`.
*   **`image`** (`*string`, Optional): The boot disk image URI.
*   **`bootDiskSizeGB`** (`*int32`, Optional): The size of the boot disk in GB.
*   **`diskType`** (`*string`, Optional): Specifies the type of the boot disk (e.g., `pd-ssd`, `pd-balanced`).
*   **`subnetwork`** (`*string`, Optional): The VPC subnetwork URI.
*   **`serviceAccount`** (`*string`, Optional): The GCP service account attached to the VMs.
*   **`networkTags`** (`[]string`, Optional): Used to apply GCP firewall rules to the TPU nodes.
*   **`metadata`** (`map[string]string`, Optional): Allows setting custom GCE metadata.

#### `BootstrapConfig` Fields

*   **`enabled`** (`bool`, Required): Indicates if the controller should bootstrap the nodes.
*   **`version`** (`*string`, Optional): The Kubernetes version to install.

### Validation Rules (CEL)

To ensure user requests are valid and can be fulfilled by GCE, the TPUNodeGroup controller will add CEL validation. It will perform the following checks during Create operations:

1.  **Project and location:** The controller will validate that the provided resources (e.g., instance template, reservation) match the project and location of the CR.
2.  **TPU Generation Restrictions:** The controller will only support `v6e` and `v7x` generations.
3.  **Reservation Requirements:** Validates the reservation field is specified if the reservation bound provisioning model is used.
4.  **Instance Template:** Validates either the instance template name is provided or the fields for creating one, not both.

*(Note: The controller will rely on the GCE API for other validations such as topology and machine type compatibility. Any errors from the GCE API will be propagated to the user through the CRD status message.)*

**Immutability:** Due to the physical constraints of TPU hardware, the `TPUNodeGroupSpec` is designed to be completely immutable after creation.
---

## `TPUNodeGroup` Status

The `TPUNodeGroup` Status provides a high-level, aggregated view of the entire slice's health.

### Fields

*   **`nodes`** (`object`): Aggregated node summary counts.
    *   **`totalNodes`** (`int32`): Count of the desired number of nodes.
    *   **`readyNodes`** (`int32`): Count of nodes in a ready state.
    *   **`provisioningNodes`** (`int32`): Count of nodes currently provisioning.
    *   **`failed`** (`int32`): Count of nodes in a failed state.
*   **`conditions`** (`list of objects`): High-level condition tracking.
    *   **`type`** (`string`): The condition type (`Ready`, `Failed`, `Reconciling`).
    *   **`status`** (`string`): The status of the condition (`True`, `False`, `Unknown`).
    *   **`reason`** (`string`): A machine-readable, CamelCase reason for the condition's last transition.
    *   **`message`** (`string`): A human-readable message indicating details about the transition.
    *   **`lastTransitionTime`** (`string`): Timestamp of when the condition last changed state.

### Supported Status Conditions

The controller utilizes three primary condition types to communicate the lifecycle and health of the node group.

| Condition Type | Purpose (Single-Host) | Purpose (Multi-Host) |
| :---- | :---- | :---- |
| **`Ready`** | `True` when `readyNodes >= spec.minReadyNodes`. Unblocks training jobs once the minimum usable capacity is reached. | `True` only when *all* nodes are ready and the TPU slice is fully formed and ICI is healthy. Atomic "all-or-nothing" readiness. |
| **`Failed`** | Indicates systemic errors (IAM, Invalid Spec) or if `MinReadyNodes` was not reached within the `provisioningTimeout`. Indicates that user intervention is required. | Indicates systemic errors or if the entire atomic slice failed to form within the `provisioningTimeout`. Indicates that user intervention is required. |
| **`Reconciling`** | `True` while the controller is still attempting to provision nodes up to the full `spec.nodeCount`, or waiting for bootstrapping. | `True` while orchestrating the bulk MIG creation and waiting for the mandatory atomic slice formation. |

### Status Transitions

The following table describes how the `Ready`, `Failed`, and `Reconciling` conditions, along with their `Reason` codes, transition across various lifecycle events.

| Scenario | Ready | Failed | Reconciling | Reason |
| :---- | :---: | :---: | :---: | :---- |
| CR Creation | `Unknown` | `False` | `True` | `Initializing` |
| Creating GCE Resources | `False` | `False` | `True` | `GCEResourceCreating` |
| GCE MIG Created, Waiting for Boot | `False` | `False` | `True` | `AwaitingCapacity` |
| Nodes Running, Waiting for K8s Join | `False` | `False` | `True` | `AwaitingNodeJoin` |
| MinNodesReached (single-host only) | `True` | `False` | `True` | `MinNodesReached` |
| Full Fulfillment Reached | `True` | `False` | `False` | `FullyProvisioned` |
| Partial stockout (Ready Nodes > Min) | `True` | `False` | `True` | `Degraded` |
| Transient GCE API Error (500/429) | `False` | `False` | `True` | `TransientAPIError` |
| IAM Permission Denied (Terminal) | `False` | `True` | `False` | `IAMPermissionDenied` |
| Invalid Spec (e.g., Bad Topology) | `False` | `True` | `False` | `InvalidSpec` |
| Provisioning Timeout Reached | `False` | `True` | `False` | `ProvisioningTimeout` |
| Termination: Cordoning Nodes | `False` | `False` | `True` | `CordoningNodes` |
| Termination: Deleting GCE Resources | `False` | `False` | `True` | `DeletingGCE` |
| Termination: Finalizing K8s Objects | `False` | `False` | `True` | `DeletingNodes` |

---
