# TPU Operator: Kubeadm Bootstrap Token Reuse and Garbage Collection

This document details the analysis of the scalability issue identified in bug **b/529339685** and provides a detailed design for handling the garbage collection of kubeadm bootstrap token secrets once a `TPUNodeGroup` is deleted.

---

## 🔍 Part 1: Analysis of the Scalability Issue (Bug b/529339685)

### Root Cause
When scaling or provisioning a `TPUNodeGroup` with $N$ nodes, individual GCE VMs do not start at the exact same millisecond. Each time a VM's state changes or a reconciliation triggers, the controller executes the reconciliation loop.
Inside the reconciliation loop, [injectMetadata](file:///google3/learning/gemini/agents/projects/smith/internal/controllers/tpunodegroup/metadata.go#L37) loops through all instances of the group. If an instance has already joined or has metadata, it keeps its token. For instances that do not have a token yet, a new token is generated *if* the local `token` variable is empty:
```go
var token string
// ...
if !hasToken {
    if token == "" {
        token, err = GenerateBootstrapToken(ctx, k8sClient)
        // ...
    }
    // ...
}
```
While this correctly reuses the token for multiple instances processed *within the same reconciliation window*, any instance transitioning to `RUNNING` in a **subsequent** reconciliation window triggers a new execution where `token` is re-initialized to `""`.
This leads to the creation of a new Kubernetes Secret (`bootstrap-token-xxxxxx` in the `kube-system` namespace) for almost every node in large slices, placing unnecessary write load on the API server and bloating `etcd`.

### Proposed Fix: Token Reuse
Instead of generating a new token from scratch in every reconciliation loop, we should:
1. Label the generated token Secrets with the `TPUNodeGroup` identity (`cloud.google.com/tpu-node-group-namespace` and `cloud.google.com/tpu-node-group-name` labels).
2. Before generating a new token, search for existing, valid (unexpired) Secrets with those labels.
3. Reuse the token if it has sufficient remaining lifetime (e.g., > 10 minutes).
4. Fall back to generating a new token only when no valid token is found.

---

## 🗑️ Part 2: Garbage Collection of Bootstrap Secrets

When a `TPUNodeGroup` is deleted, we must clean up the bootstrap token secrets. Leftover active bootstrap secrets in `kube-system` are a security risk and contribute to resource bloat.

### ⚠️ The Cross-Namespace OwnerReference Limitation
In Kubernetes, **cross-namespace `ownerReferences` are not supported**. 
* The `TPUNodeGroup` resource resides in a user-specified namespace (e.g., `default` or `tpu-system`).
* The kubeadm bootstrap secrets must reside in the `kube-system` namespace to be recognized by the Kubernetes Bootstrap Authenticator.
Because they reside in different namespaces, we **cannot** set the `ownerReference` of the Secret to point directly to the `TPUNodeGroup`. If we do, Kubernetes will disallow it or fail to garbage collect it.

### Proposed Garbage Collection Strategy: Controller-Managed Finalizer
We will handle garbage collection within the `TPUNodeGroup` controller itself during resource deletion:
1. **Namespace-Safe Labeling**: We will label the bootstrap secrets using two separate labels: `cloud.google.com/tpu-node-group-namespace: <namespace>` and `cloud.google.com/tpu-node-group-name: <name>`. This approach (detailed in [safe_labels_implementation_plan.md](file:///usr/local/google/home/ypgao/gke-labs/copy-tpu/docs/safe_labels_implementation_plan.md)) ensures uniqueness and prevents cross-namespace collisions while staying safely under the 63-character Kubernetes limit without requiring hashing.
2. **Deletion Finalizer Hook**: The `TPUNodeGroup` controller uses finalizers to coordinate the teardown of child resources (MIG, InstanceTemplates, etc.). We will perform bootstrap secret cleanup as part of the `tpu.google.com/cleanup-nodes` finalizer phase in the controller's `handleDeletion` flow.
3. **RBAC Update**: The controller's RBAC role is updated to allow `list` and `delete` operations on `secrets` in the `kube-system` namespace.

---

## ⚡ Part 3: CI/CD Robustness Improvements

To unblock E2E tests in resource-constrained or high-latency GCP zones (e.g., `us-central1-c`), we implemented several operational improvements:

### 1. Early Metadata Injection
Previously, the controller waited for the Managed Instance Group to reach a "Stable" state before injecting metadata. In some zones, instances can remain in `PENDING` or `PROVISIONING` for minutes. By moving `injectMetadata` *before* the stability check and allowing injection into non-`RUNNING` instances, we ensure the `kubeadm-join-token` is available the moment the VM finishes booting, avoiding the 2.5-minute timeout in the startup script.

### 2. Transient Failure Resilience
MIG creation often fails with `ZONE_RESOURCE_POOL_EXHAUSTED`. We reclassified `ReasonInstancesCreationFailed` as a transient failure. This allows the operator to retry automatically every 10 seconds rather than entering a 10-minute "Terminal Failure" state, significantly speeding up E2E tests when capacity eventually becomes available.


---

## 🙋 Frequently Asked Questions

### Do we really need to label the bootstrap secrets?
**Yes.** Without labels, the controller would have no clean way to:
1. Identify which token Secret belongs to which `TPUNodeGroup` when retrieving it during subsequent reconciliations to perform reuse.
2. Target and delete only the relevant secrets belonging to a deleted `TPUNodeGroup`. Without labels, the controller would either leave leaked secrets indefinitely in `kube-system` or be forced to parse all secrets in the namespace, risking accidental deletion of other components' bootstrap tokens.

### What label keys and values should we use?
* **Namespace Label**: `cloud.google.com/tpu-node-group-namespace` (stores the namespace of the `TPUNodeGroup`).
* **Name Label**: `cloud.google.com/tpu-node-group-name` (stores the name of the `TPUNodeGroup`).
  Using separate labels for namespace and name ensures both full human-readability and guaranteed adherence to the 63-character limit. It avoids cross-namespace collisions (e.g., if two node groups in different namespaces share the same name) without the need for hashing, as both namespaces and `TPUNodeGroup` names (constrained by GCE limits) are individually capped at 63 characters.
