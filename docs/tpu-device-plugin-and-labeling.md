# TPU Device Plugin & Labeling

## Overview

The TPU Device Plugin is a Kubernetes DaemonSet deployed across all TPU-equipped nodes. It serves as the authoritative bridge between the host's physical PCIe/system buses and the Kubelet, enabling the advertising and allocation of `google.com/tpu` resources.

For self-managed clusters (gK8s), this plugin will be a stripped-down version of the GKE TPU Device Plugin found at `https://source.corp.google.com/h/gke-internal/kubernetes/cloud-tpu/+/master:`. All GKE-specific telemetry, metrics pipelines, and proprietary integrations will be removed to provide a stable, open-source-friendly solution.

---

## 1. Delegated Node Labeling

Because the TPU Device Plugin is the only component that can cryptographically and physically verify the health of the attached accelerators, it is delegated the responsibility of labeling the Kubernetes `Node` object.

This ensures the Kubernetes scheduler does not place distributed ML workloads on a Node until its hardware is verified as healthy.

### Metadata Labels

If the hardware is healthy, the plugin executes an HTTP `GET` request to the local GCE Metadata Server to retrieve topology variables injected by the TPUNodeGroup controller.

Specifically, it looks for the following keys prefixed with `cloud.google.com/gk8s-tpu-`:
*   `cloud.google.com/gk8s-tpu-accelerator` (e.g., `tpu-v7-standard-4t`)
*   `cloud.google.com/gk8s-tpu-topology` (e.g., `4x4x4`)
*   `cloud.google.com/gk8s-tpu-slice-[TOPOLOGY]-id` (e.g., a unique slice UUID)

The plugin then applies these variables as labels to its own Kubernetes `Node` object.

---

## 2. Kubernetes RBAC Requirements

To execute the delegated labeling workflow, the TPU Device Plugin's `ServiceAccount` requires explicit RBAC permissions to modify `Node` objects.

Because `Node` objects are cluster-scoped, a `ClusterRole` and `ClusterRoleBinding` are strictly required.

### Required ClusterRole Definition

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: tpu-device-plugin
rules:
- apiGroups: [""]
  resources: ["nodes"]
  # The plugin reads and patches node labels.
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  # The plugin needs to list and watch pods on the node to extract TPU container info.
  verbs: ["get", "list", "watch"]
```