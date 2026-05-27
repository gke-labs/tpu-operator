# WorkloadPolicy Controller (Lower Layer Provisioner)

## Context & Purpose
*   **Role:** Low-level GCE infrastructure provisioner in the Composite Pattern (translates `WorkloadPolicy` CR -> GCE Resource Policy).

## Core Behaviors
*   **Field Mapping:** Converts `WorkloadPolicy` Spec -> `computepb.ResourcePolicy` (Workload Policy type).
*   **Non-Blocking Reconciliation:** (Planned) Utilizes asynchronous polling for GCE operations.
*   **Lifecycle:** Finalizer ensures GCE resource deletion before CR removal.

## Status Patching Pattern
This controller adopts the centralized deferred status patching pattern:
1. Fetch object.
2. `DeepCopy` to `base`.
3. `defer` status patching using `client.MergeFrom(base)`.
4. Logic modifies object in-memory.
