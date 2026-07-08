# Safe Label Values: Multi-Label Implementation Plan

This document consolidates the proposed changes for using a multi-label approach to safely associate resources (Nodes and Secrets) with a `TPUNodeGroup` without exceeding Kubernetes label value length limits.

---

## 🏗️ 1. Proposed Plan: Multi-Label Approach

To avoid exceeding the 63-character limit on Kubernetes label values for long names, and to keep label values fully human-readable, we will replace the single, concatenated label with two separate labels on all associated resources:

*   **Namespace Label**: `cloud.google.com/tpu-node-group-namespace` (stores the namespace of the `TPUNodeGroup`)
*   **Name Label**: `cloud.google.com/tpu-node-group-name` (stores the name of the `TPUNodeGroup`)

### 📏 Why this completely avoids the 63-character limit:
1.  **Namespace**: Kubernetes namespaces have a hard limit of **63 characters**. Thus, the namespace value will always fit.
2.  **Name**: Although Kubernetes CRD names can be up to 253 characters, a `TPUNodeGroup` name is constrained by **GCE resource naming limits (63 characters)** because the controller creates child GCE resources (Instance Templates, MIGs) with suffixes. The most restrictive suffix is `-template` (9 characters), meaning the `TPUNodeGroup` name is effectively limited to **54 characters** for any successful provisioning.

Since both the namespace (max 63 chars) and name (max 54 chars) are guaranteed to be under 63 characters individually, their values will always fit within Kubernetes label limits.

---

## 🔄 2. Codebase-Wide Consistency Integration

We will define two new label constants package-wide and consistently integrate them across our controller logic.

### Constants (`internal/controllers/tpunodegroup/node.go`)
We will remove `labelTPUNodeGroup` and define:
```go
const (
	labelTPUNodeGroupNamespace = "cloud.google.com/tpu-node-group-namespace"
	labelTPUNodeGroupName      = "cloud.google.com/tpu-node-group-name"
)
```

### Integration Locations:

1.  **`internal/controllers/tpunodegroup/node.go`**:
    Update `ensureNodeLabels` to verify and set both labels on GKE Node objects:
    ```go
    if val, ok := node.Labels[labelTPUNodeGroupNamespace]; !ok || val != group.Namespace {
        needsUpdate = true
    }
    if val, ok := node.Labels[labelTPUNodeGroupName]; !ok || val != group.Name {
        needsUpdate = true
    }
    // ...
    node.Labels[labelTPUNodeGroupNamespace] = group.Namespace
    node.Labels[labelTPUNodeGroupName] = group.Name
    ```

2.  **`internal/controllers/tpunodegroup/deletion.go`**:
    *   Update `cordonNodes` when listing nodes to cordon:
        ```go
        labelSelector := client.MatchingLabels{
            labelTPUNodeGroupNamespace: group.Namespace,
            labelTPUNodeGroupName:      group.Name,
        }
        ```
    *   Update `deleteNodeObjects` when listing nodes to delete:
        ```go
        labelSelector := client.MatchingLabels{
            labelTPUNodeGroupNamespace: group.Namespace,
            labelTPUNodeGroupName:      group.Name,
        }
        ```
    *   Update `deleteBootstrapSecrets` when listing secrets to delete:
        ```go
        listOpts := []client.ListOption{
            client.InNamespace("kube-system"),
            client.MatchingLabels{
                labelTPUNodeGroupNamespace: group.Namespace,
                labelTPUNodeGroupName:      group.Name,
            },
        }
        ```

3.  **`internal/controllers/tpunodegroup/metadata.go`**:
    *   Update `GetOrGenerateBootstrapToken` to filter by both labels when fetching existing tokens:
        ```go
        listOpts := []client.ListOption{
            client.InNamespace("kube-system"),
            client.MatchingLabels{
                labelTPUNodeGroupNamespace: group.Namespace,
                labelTPUNodeGroupName:      group.Name,
            },
        }
        ```
    *   Update the generated bootstrap token secret to carry both labels:
        ```go
        labels := map[string]string{
            labelTPUNodeGroupNamespace: group.Namespace,
            labelTPUNodeGroupName:      group.Name,
        }
        return GenerateBootstrapToken(ctx, k8sClient, labels)
        ```
