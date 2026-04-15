# Node Bootstrapping

## Overview

The Node Bootstrapping module handles the secure delivery of cluster credentials and the execution of startup scripts on newly provisioned TPU VMs.

This document details the configuration of the GCE `startup-script` metadata, securely generating `kubeadm` join tokens via the Kubernetes API, and injecting both the token and the dynamic control plane endpoint into VM metadata using the "Pull" model.

---

## 1. Startup Script Injection (`startup-script`)

When `spec.bootstrapKubernetes.enabled: true`, the controller injects a bash script into the Managed Instance Group's (MIG) `allInstancesConfig` metadata under the key `startup-script`.

By injecting the script at the MIG level rather than modifying the Instance Template, the controller ensures that it never alters a user's reusable, corporate-standard Instance Template, while still providing managed bootstrapping exclusively for this TPU Node Group.

### Script Responsibilities
The script is embedded as a constant or read from a local asset file in the controller's binary. It performs the following tasks:
1.  **Install Dependencies:** Install container runtime (e.g., `containerd`), `socat`, `conntrack`, and the Kubernetes toolchain (`kubelet`, `kubeadm`, `kubectl`) matching `spec.version`.
2.  **Poll for Token, Endpoint, and CA Hash:** Continuously poll the local GCE Metadata Server until the `kubeadm-join-token`, `kubeadm-control-plane`, and `kubeadm-ca-hash` keys appear.
    ```bash
    TOKEN=""
    ENDPOINT=""
    HASH=""
    while [ -z "$TOKEN" ] || [ -z "$ENDPOINT" ] || [ -z "$HASH" ]; do
      TOKEN=$(curl -s -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kubeadm-join-token" || true)
      ENDPOINT=$(curl -s -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kubeadm-control-plane" || true)
      HASH=$(curl -s -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kubeadm-ca-hash" || true)
      if [ -z "$TOKEN" ] || [ -z "$ENDPOINT" ] || [ -z "$HASH" ]; then
        sleep 5
      fi
    done
    ```
3.  **Kubeadm Join:** Execute `kubeadm join` using the retrieved token, control plane endpoint, and CA hash.
    ```bash
    kubeadm join "$ENDPOINT" --token "$TOKEN" --discovery-token-ca-cert-hash "sha256:$HASH"
    ```

---

## 2. Token Generation (Kubernetes API)

The controller does not use a static, long-lived token. It generates a short-lived `BootstrapToken` dynamically.

### Implementation Details
The controller uses the Kubernetes `client-go` library to create a Secret in the `kube-system` namespace formatted specifically for `kubeadm`.

1.  **Generate Token ID and Secret:** Generate a random 6-character string (`[a-z0-9]{6}`) for the token ID, and a 16-character string (`[a-z0-9]{16}`) for the token secret. The full token is `{id}.{secret}`.
2.  **Create Bootstrap Secret:**
    *   **Namespace:** `kube-system`
    *   **Name:** `bootstrap-token-{id}`
    *   **Type:** `bootstrap.kubernetes.io/token`
    *   **Data:**
        *   `token-id`: `{id}`
        *   `token-secret`: `{secret}`
        *   `expiration`: Set to a short TTL (e.g., `time.Now().Add(15 * time.Minute)` formatted as RFC3339).
        *   `usage-bootstrap-authentication`: `"true"`
        *   `usage-bootstrap-signing`: `"true"`
        *   `auth-extra-groups`: `"system:bootstrappers:kubeadm:default-node-token"`

---

## 3. The "Pull" Token and Endpoint Injection Workflow

To avoid baking tokens and potentially stale control plane IP addresses into templates that might sit in `PROVISIONING` for days during a stockout, the controller injects these values *only* when the VM boots.

### Controller Logic

1.  **State Check:** During reconciliation, check the `TPUNodeState`. If `gceInstanceStatus == "RUNNING"` and `tokenState == "Pending"`:
2.  **Generate Token:** Call the internal token generation function (see Section 2) to get a fresh `{id}.{secret}` token.
3.  **Resolve Endpoint & CA Hash:** Dynamically fetch the current control plane endpoint and the CA certificate hash from the cluster.
4.  **Inject Metadata:** Call the GCE API Client to `SetMetadata` on that specific VM instance.
    *   **Key 1:** `kubeadm-join-token`, **Value:** The generated token string.
    *   **Key 2:** `kubeadm-control-plane`, **Value:** The resolved control plane endpoint (e.g., `10.0.0.1:6443`).
    *   **Key 3:** `kubeadm-ca-hash`, **Value:** The SHA-256 hash of the CA certificate.
5.  **Update State:** Update the `TPUNodeState` CR to set `tokenState = "Injected"`.

---

## 4. Metadata Cleanup

Once a node has successfully joined the cluster, leaving the bootstrap token in the VM's plaintext metadata is a security risk.

### Controller Logic

1.  **State Check:** During reconciliation, check the `TPUNodeState`. If `kubernetesNodeReady == true` and `tokenState == "Injected"`:
2.  **Remove Metadata:** Call the GCE API Client to `SetMetadata` on the specific VM instance, explicitly omitting or removing the `kubeadm-join-token`, `kubeadm-control-plane`, and `kubeadm-ca-hash` keys.
3.  **Delete Secret (Optional):** Delete the corresponding `bootstrap-token-{id}` Secret from the `kube-system` namespace (though the short TTL will auto-expire it).
4.  **Update State:** Update the `TPUNodeState` CR to set `tokenState = "CleanedUp"`.