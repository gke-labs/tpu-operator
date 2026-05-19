# Deployment Manifests

This directory contains manifests for deploying the TPU Node Group Controller and the TPU Device Plugin.

## Installation

To install the controller and device plugin, follow these steps:

1.  **Apply CRDs**:
    Ensure you have the Custom Resource Definitions applied.
    ```bash
    kubectl apply -f crds/
    ```

2.  **Apply Generated RBAC Roles**:
    The RBAC roles are generated from code markers and are stored in the `rbac` directory.
    ```bash
    kubectl apply -f rbac/controller/role.yaml
    kubectl apply -f rbac/deviceplugin/role.yaml
    ```

3.  **Apply Main Manifests**:
    Apply the `install.yaml` file which contains the Namespace, ServiceAccounts, RoleBindings, and the Controller Deployment.
    ```bash
    kubectl apply -f install.yaml
    ```

## Regeneration

If you update the RBAC markers in the controller or device plugin code, you must regenerate the roles using:
```bash
make manifests
```
from the root of the repository. This will update the files in `deploy/crds` and `deploy/rbac`.
