# TPU Node Group Controller - End-to-End (E2E) Tests

This directory contains the End-to-End (E2E) test suite for the TPU Node Group Controller. The tests validate the complete controller lifecycle—from Custom Resource (CR) application in a Kubernetes cluster to the actual provisioning and cleanup of Google Compute Engine (GCE) resources.

## 🏛 Architecture Overview

Unlike mock-based unit tests, the E2E test suite operates against a live local Kubernetes cluster and interacts with real GCP infrastructure.

### Execution Flow

1. **Cluster Preparation (`setup-e2e.sh`)**: Initializes a local KinD cluster (`tpu-controller-e2e`) and applies the latest CRDs.
2. **Controller Startup (`TestMain`)**: Launches the controller locally in the background (`go run cmd/main.go`), pointing its kubeconfig to the local KinD cluster.
3. **Test Reconcile & Verification**:
   - **Kubernetes State**: Tests apply manifests via `kubectl` and verify Custom Resource conditions.
   - **GCP Infrastructure**: Tests call `gcloud` CLI to verify actual cloud assets (e.g., Instance Templates, Managed Instance Groups) in the test GCP project.
4. **Teardown (`TestMain`)**: Kills the local controller background process and cleans up resources upon completion.

### Key Components

*   **`setup-e2e.sh`**: Prepares the test environment by verifying GCP Application Default Credentials (ADC), spinning up a local KinD cluster (`tpu-controller-e2e`), and installing the latest CRD manifests.
*   **`e2e_test.go`**: The test framework harness. `TestMain` automatically launches the controller locally (`go run cmd/main.go`) connected to the KinD cluster, executes all test suites, and ensures process teardown upon completion. Logs are written to `/tmp/controller_e2e.log`.
*   **Resource Tests (`*_test.go`)**: Individual test files (e.g., `tpunodegroup_test.go`, `instancetemplate_test.go`) that execute `kubectl` and `gcloud` CLI commands to verify K8s reconciliation and GCP resource lifecycle (creation and deletion).

## 📋 Prerequisites

Ensure the following tools are installed on your host machine:
*   `go` (1.22+)
*   `docker` & `kind`
*   `kubectl`
*   `gcloud` CLI configured with active permissions in the GCP test project (`gsc-nexus-xteam-shared-testing`).

## 🚀 Running E2E Tests

### 1. Setup Test Environment
Authenticate with GCP and initialize the local KinD cluster:
```bash
# Login to Application Default Credentials (ADC)
gcloud auth application-default login

# Create KinD cluster and install CRDs
./hack/setup-e2e.sh
```

### 2. Run Test Suite
Run all E2E tests:
```bash
go test -v -tags=e2e ./hack/e2e/...
```

Alternatively, you can use the Makefile target:
```bash
make e2e-test
```

*To run a specific test case:*
```bash
go test -v -tags=e2e ./hack/e2e/... -run TestTPUNodeGroup_MultiHost
```

## 🔍 Debugging & Troubleshooting

*   **Controller Logs**: The locally spawned controller outputs its runtime logs to `/tmp/controller_e2e.log`. Inspect this file if reconciliation times out or fails.
*   **Live VM Boot & Startup Script Debugging**: If the test times out waiting for nodes to join (e.g., `Ready=0, Reconciling=1`), it usually means the GCE VM failed to bootstrap Kubernetes. You can inspect the live boot and startup script progress of the provisioning VM:
    1.  Find the provisioning VM name (e.g. matching `test-nodegroup-mig-*` or `test-multihost-mig-*`):
        ```bash
        gcloud compute instances list --project="gsc-nexus-xteam-shared-testing" --filter="name~test-nodegroup"
        ```
    2.  Pull the serial port logs live while the VM is booting (look for package installs or `kubeadm join` failures):
        ```bash
        gcloud compute instances get-serial-port-output <vm-name> --zone="us-central1-c" --project="gsc-nexus-xteam-shared-testing"
        ```
*   **Lingering Resources**: Tests attempt to clean up resources before and after execution via `cleanResources`. If a test fails mid-execution, inspect the KinD cluster directly using `kubectl --context kind-tpu-controller-e2e get tpunodegroup`.
*   **GCP Permission Errors**: If `gcloud compute` verification steps fail with 403 errors, re-run `gcloud auth application-default login` and ensure your Google account has Editor/Viewer access to the test GCP project.
