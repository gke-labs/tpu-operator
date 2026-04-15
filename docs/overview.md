# High-Level Overview: TPU Node Group Controller

## Objective

The objective of the **TPU Node Group Controller** is to manage the lifecycle of TPU resources within self-managed Kubernetes clusters running on Google Compute Engine (GCE). It provides a Kubernetes-native API (via Custom Resource Definitions) to automate the provisioning, deletion, health monitoring, and management of GCE TPU slices, making them available as schedulable resources in Kubernetes.

For Phase One, the focus is on:
*   Supporting TPU v6e and v7x generations.
*   Static slicing
*   Provisioning and graceful deletion.

## Non-Goals (Phase One)

*   Complex auto-repair logic (relies on GCE MIG autohealing)
*   Direct involvement in custom customer cluster lifecycle toolsets beyond providing the TPU nodes.

## Architecture Overview

The TPU Node Group Controller runs as an operator within a self-managed Kubernetes cluster. Users define the desired state of their TPU capacity using the `TPUNodeGroup` CRD.

The controller interacts with two primary systems:
1.  **Kubernetes API Server:** To watch for `TPUNodeGroup` resources, and manage finalizers.
2.  **Google Compute Engine (GCE) API:** To provision the underlying infrastructure, including Instance Templates, Workload Policies, and Managed Instance Groups (MIGs).

### High-Level Flow

```text
+------+       +---------+       +------------+       +---------+       +----+       +--------+
| User |       | K8s API |       | Controller |       | GCE API |       | VM |       | Plugin |
+------+       +---------+       +------------+       +---------+       +----+       +--------+
   |                |                  |                   |               |              |
   |--- Create ---->|                  |                   |               |              |
   |  TPUNodeGroup  |                  |                   |               |              |
   |                |-- Reconcile ---->|                   |               |              |
   |                |                  |--- Create WP ---->|               |              |
   |                |                  |--- Create MIG --->|               |              |
   |                |                  |                   |               |              |
   |                |                  |                   |               |              |
   |                |                  |<- Poll Status ----|               |              |
   |                |                  |--- (RUNNING) ---->|               |              |
   |                |<-- Gen Token ----|                   |               |              |
   |                |                  |--- Set Metadata ->|               |              |
   |                |                  |                   |               |              |
   |                |                  |                   |<-- Read Token |              |
   |                |<---------------------- kubeadm join -----------------|              |
   |                |                  |                   |               |              |
   |                |                  |                   |               |<-- Discover -|
   |                |                  |                   |<-- Read Labels|              |
   |                |<----------------------------- Patch Node ---------------------------|
   |                |<-- Status Ready -|                   |               |              |
+------+       +---------+       +------------+       +---------+       +----+       +--------+
```

1.  **User Request:** A user creates a `TPUNodeGroup` CR specifying the desired TPU topology, machine type, and GCP project details.
2.  **Validation:** The Kubernetes API server validates the request using CEL rules defined in the CRD (e.g., ensuring that provided resources match the project and location). The controller relies on the GCE API for topology and machine type compatibility validation.
3.  **Provisioning (Controller):**
    *   The controller creates a Workload Policy for the specified topology (if multi-host).
    *   It creates a Managed Instance Group (MIG) in bulk mode(multi-host only) using the specified or generated Instance Template.
4.  **Bootstrapping (Optional):** If enabled, the controller generates short-lived `kubeadm` join tokens and injects them into the metadata of RUNNING VMs. Startup scripts on the VMs use these tokens to join the Kubernetes cluster.
5.  **Labeling & Discovery (Device Plugin):** Once the VMs join as Kubernetes Nodes, the TPU Device Plugin DaemonSet discovers the physical TPU hardware, reads topology metadata from the GCE instance, and labels the Node object, making it ready for scheduling.
6.  **Teardown (Controller):** Upon deletion of the `TPUNodeGroup`, the controller uses a finalizer to ensure underlying GCE resources (MIGs, Workload Policies) are deleted before removing the Kubernetes Node objects.