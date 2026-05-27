---
name: TPU Node Group Controller Agent Guide
description: Guiding document for AI agents working on the TPU Node Group project.
last_updated: 2026-05-20
---

# TPU Node Group Controller

The TPU Node Group Controller is a Kubernetes operator that manages the lifecycle of TPU resources within self-managed Kubernetes clusters on Google Compute Engine.

## 🚀 Guiding Principles for AI Agents

> [!IMPORTANT]
> **Always follow these rules when contributing code:**
> 1. **Test Before You Suggest**: After changing any production code, you MUST run all relevant unit tests (e.g., `go test ./internal/controllers/...`).
> 2. **Pattern Matching**: Before implementing a new feature, search the codebase for similar implementations and apply the same patterns.
> 3. **Avoid Over-Verbosity**: Keep responses concise and focused on the task.
> 4. **Gerrit Flow**: Preserve `Change-Id` in commit messages when amending.

## 📁 Code Organization

```text
tpu-node-group/
├── cmd/
│   └── controller-manager/  # Entry point (main.go)
├── deploy/
│   └── crds/               # Auto-generated CRD manifests
├── internal/
│   ├── apis/               # API definitions (Group/Version)
│   ├── controllers/
│   │   ├── tpunodegroup/    # Main controller logic
│   │   ├── instancetemplate/ # InstanceTemplate controller logic
│   │   ├── managedinstancegroup/ # ManagedInstanceGroup controller logic
│   │   └── workloadpolicy/  # WorkloadPolicy controller logic
│   ├── converter/          # Type conversion utilities
│   └── gce/                # GCE provider interactions
├── hack/                   # Helper scripts and boilerplates
├── docs/                   # Task-specific documentation & controller basics
└── Makefile                # Build and generation tasks
```

## 🛠 Tooling & Workflow

### Development Commands
| Task | Command |
| :--- | :--- |
| **Run Tests** | `make test` |
| **Run E2E Tests** | `make e2e-test` |
| **Generate Code** | `make generate` |
| **Generate CRDs** | `make manifests` |
| **Update Dependencies** | `go mod tidy && go mod vendor` |

### Source Control
*   **Git**: Use `git` for all version control operations.
*   **Vendor**: Ignore `vendor/` in diffs by default: `git show <commit> -- . ':!vendor'`.
*   **Commits**: Follow [STYLE.md](STYLE.md) for commit message guidelines.

## 📖 Key References

*   [STYLE.md](STYLE.md): Coding standards (Go, Markdown, Commits).


