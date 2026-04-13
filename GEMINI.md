# TPU Node Group Controller

The TPU Node Group Controller is a Kubernetes operator that manages the lifecycle of TPU resources within self-managed Kubernetes clusters on Google Compute Engine. It provides a Kubernetes-native API to automate provisioning, health monitoring, and management of TPU slices, making them available as schedulable resources. For more context, see the [original design](http://go/tpus-on-self-managed-k8s).

## Code Organization

*   `/docs`: Contains the documentation for the project.

## Guidelines

*   Make sure the documentation in `docs/` is always up to date with the implementation.
