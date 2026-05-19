# TPU Node Group Controller - End-to-End (E2E) Tests

This directory contains the End-to-End (E2E) test suite for the TPU Node Group Controller. The tests validate the complete controller lifecycle—from Custom Resource (CR) application in a Kubernetes cluster to the actual provisioning and cleanup of Google Compute Engine (GCE) resources.

## 🏛 Architecture Overview

Unlike mock-based unit tests, the E2E test suite operates against a live Kubernetes cluster running on GCE VMs and interacts with real GCP infrastructure.

### Execution Flow

1.  **Cluster Connection (`connect-gce-cluster.sh`)**: Establishes an SSH tunnel to a remote GCE control plane node and fetches the localized `kubeconfig`.
2.  **Safety Check (`TestMain`)**: Before running any tests, the suite verifies that the target cluster's control-plane IP matches the `controlPlaneIP` specified in the test manifest (e.g., `pkg/controllers/tpunodegroup/testdata/test_nodegroup.yaml`). This prevents accidentally running tests against the wrong cluster.
3.  **Controller Startup (`TestMain`)**: Builds and launches the controller locally in the background, pointing its kubeconfig to the remote cluster via the established tunnel.
4.  **Test Reconcile & Verification**:
    *   **Kubernetes State**: Tests apply manifests to the remote cluster and verify Custom Resource conditions.
    *   **GCP Infrastructure**: Tests call `gcloud` CLI to verify actual cloud assets (e.g., Instance Templates, Managed Instance Groups) in the target GCP project.
5.  **Teardown (`TestMain`)**: Terminates the local controller process and cleans up Kubernetes resources.

## 📋 Prerequisites

Ensure the following tools are installed on your host machine:
*   `go` (1.22+)
*   `kubectl`
*   `gcloud` CLI configured with active permissions in the target GCP project.

## 🚀 Running E2E Tests

### 1. Identify Target Cluster
Ensure you have a GCE-based K8s cluster running. You will need:
*   `CONTROL_PLANE_NODE`: The name of the GCE instance running the control plane.
*   `PROJECT`: The GCP project ID.
*   `ZONE`: The zone where the control plane node is located.

### 2. Establish Connection
Use the connection script to set up the SSH tunnel and local environment:
```bash
export CONTROL_PLANE_NODE="your-control-plane-node"
export PROJECT="your-project"
export ZONE="your-zone"
./hack/e2e/connect-gce-cluster.sh
```
This script will export `KUBECONFIG`, `E2E_PROJECT`, and `E2E_ZONE` to your current shell.

### 3. Run Test Suite
Run all E2E tests:
```bash
go test -v -tags=e2e ./hack/e2e/...
```

*To run a specific test case (e.g., Single-Host):*
```bash
go test -v -tags=e2e ./hack/e2e/... -run TestTPUNodeGroup
```

## 🔍 Debugging & Troubleshooting

*   **Safety Errors**: If you see `SAFETY ERROR: ... control-plane IP does NOT match`, ensure your `KUBECONFIG` is pointing to the correct cluster and that the `controlPlaneIP` in your test manifest is accurate.
*   **Controller Logs**: Runtime logs are written to `/tmp/controller_e2e.log`. Inspect this file if reconciliation timeouts occur.
*   **Serial Port Logs**: If nodes fail to join the cluster, inspect the boot and startup script progress:
    ```bash
    gcloud compute instances get-serial-port-output <vm-name> --zone="us-central1-c" --project="your-project"
    ```
*   **SSH Tunnel**: The `connect-gce-cluster.sh` establishes a background tunnel on port 6443. If you encounter "connection refused" errors, check if the SSH process is still running: `pgrep -af ssh`.
