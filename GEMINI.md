# TPU Node Group Controller

The TPU Node Group Controller is a Kubernetes operator that manages the lifecycle of TPU resources within self-managed Kubernetes clusters on Google Compute Engine. It provides a Kubernetes-native API to automate provisioning, health monitoring, and management of TPU slices, making them available as schedulable resources.

## Code Organization

> [!IMPORTANT]
> Ensure that this code organization structure is kept up-to-date with each new change that modifies the project layout.

```text
tpu-node-group/
├── cmd/
│   └── controller-manager/
│       └── main.go             # Entry point
├── deploy/
│   ├── crds/
│   │   ├── _tpunodegroups.yaml
│   │   └── _anotherresources.yaml
│   └── operator/
├── pkg/
│   ├── apis/
│   │   └── tpu/
│   │       └── v1alpha1/
│   ├── controllers/
│   │   └── tpunodegroup/
│   └── generated/
├── hack/
└── docs/                   # Project docs (Markdown), useful for AI agents
```

## Source Code

* This repository is **NOT** Google3, so please do not follow any Google3 specific workflows and guidelines.
* This is a git repo on a Linux workstation.
* To read a `google3` path, use `/google/src/files/head/depot/google3/...`.
* As a general rule, remember that the private-cloud repository follows some different conventions than many
  examples in your training data.
* If you ever get confused why something is not working the way you expect, confirm any assumptions about
  code conventions or interfaces using documentation and/or examples elsewhere in the codebase.
* Go is the main programming language for this repository.
* Go vendor code is under `vendor/`.
* Files in `docs/*.md` are for specific tasks. Do not read them by default unless they are relevant to your current task.
* Prefer to use third-party dependencies that you find in other examples within the codebase, and only use
  external repository labels (like `"@com_google_protobuf//:any_proto"`) if you can find examples using the
  same label elsewhere in this codebase.
* The importpath for code in this repository starts with `gke-internal.googlesource.com`
* **Adding Dependencies**: You can add new dependencies by importing them in your code, then running `go mod tidy` to update `go.mod`, and `go mod vendor` to update the `vendor/` directory.

## Code Generation

This project relies on automatic code generation for Kubernetes clients to maintain consistency and reduce boilerplate.

*   **Tool**: Kubernetes `code-generator` (generating `clientset`, `informers`, `listers`, and `applyconfigurations`).
*   **Structure**: The API types are located in `pkg/apis/tpu/v1alpha1/` to comply with the `code-generator` package layout requirements (which expects `pkg/apis/<group>/<version>`).
*   **Decision**: The decision to use `code-generator` was made to adhere to standard Kubernetes patterns for custom resources when not using higher-level frameworks like Kubebuilder. This allows the project to maintain full control over the controller implementation (using pure `client-go`) while leveraging standard tooling for client generation.
*   **Workflow**: Run `make codegen-client` to update generated code after modifying API types in `pkg/apis/tpu/v1alpha1/`.

## Source Control

* Use Git for source control.
* When amending a commit, ensure that the `Change-Id` field in the commit message is preserved to update the existing Gerrit change rather than creating a new one.
* When reading a commit or viewing diffs, ignore changes under the `vendor/` folder by default to focus on relevant code modifications (e.g., use `git show <commit> -- . ':!vendor'`).


## Style Guide

See [STYLE.md](STYLE.md) for the style guide, including Go style, markdown style, and commit message etiquette.

## References

*   See [DESIGN.md](DESIGN.md) for key design documents.
*   **Important**: Before reading any Google Doc, you MUST ask the user for explicit approval.
