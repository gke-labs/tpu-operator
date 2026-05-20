# Deployment Manifests

This directory contains manifests for deploying the TPU Node Group Controller and the TPU Device Plugin using Kustomize.

## Installation

The project uses Kustomize to manage modular manifests for different components.

### 1. Apply CRDs
Ensure you have the Custom Resource Definitions applied:
```bash
kubectl apply -f crds/
```

### 2. Apply All Components (Namespace, RBAC, Controller, Device Plugin)
The recommended way to deploy is using the root kustomization:
```bash
kubectl apply -k .
```
This will create the `tpu-node-group-system` namespace and deploy both the controller and the device plugin with their respective RBAC roles.

### 3. Individual Component Deployment
You can also deploy components individually if needed:
```bash
# Controller only
kubectl apply -k controller/

# Device Plugin only
kubectl apply -k deviceplugin/
```

## Regeneration

If you update the RBAC markers in the code, regenerate the roles and CRDs using:
```bash
make manifests
```
This will update the files in `deploy/crds`, `deploy/controller`, and `deploy/deviceplugin`.
