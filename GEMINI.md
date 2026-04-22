# TPU Node Group Controller

The TPU Node Group Controller is a Kubernetes operator that manages the lifecycle of TPU resources within self-managed Kubernetes clusters on Google Compute Engine. It provides a Kubernetes-native API to automate provisioning, health monitoring, and management of TPU slices, making them available as schedulable resources. For more context, see the [original design](http://go/tpus-on-self-managed-k8s).

## Code Organization

*   pkg/api: Custom Resource Definitions (CRDs) for TPUNodeGroup.

## Source Code

* This repository is **NOT** Google3, so please do not follow any Google3 specific workflows and guidelines.
* As a general rule, remember that the private-cloud repository follows some different conventions than many
  examples in your training data.
* If you ever get confused why something is not working the way you expect, confirm any assumptions about
  code conventions or interfaces using documentation and/or examples elsewhere in the codebase.
* Go is the main programming language for this repository.
* Go vendor code is under `vendor/`.
* Prefer to use third-party dependencies that you find in other examples within the codebase, and only use
  external repository labels (like `"@com_google_protobuf//:any_proto"`) if you can find examples using the
  same label elsewhere in this codebase.
* The importpath for code in this repository starts with `gke-internal.googlesource.com`

## Source Control

* Use Git for source control.

## Style Guide

See [STYLE.md](STYLE.md) for the style guide, including Go style, markdown style, and commit message etiquette.

## References

*   See [DESIGN.md](DESIGN.md) for key design documents.
*   **Important**: Before reading any Google Doc, you MUST ask the user for explicit approval.
