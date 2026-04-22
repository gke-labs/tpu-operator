# Direct client-go Controller Basics

This document outlines the minimum requirements for a direct `client-go` controller in Kubernetes and how different components fit together.

## Core Problem
The fundamental problem a controller solves is: **How to watch resources in Kubernetes and react to changes without overloading the API server?** Continuous polling would be too heavy, so Kubernetes uses an event-driven architecture.

## Core Components
Here are the 5 key components that work together:

### 1. The Informer (The Listener and Cacher)
*   **What it is:** A background process that maintains a live connection (a "watch") to the Kubernetes API server.
*   **Minimum Requirement:** You need an Informer for *every* resource type you want to watch.
*   **How it works:** When a resource is created, updated, or deleted, the API server tells the Informer, which stores it in a **local in-memory cache**.
*   **Why:** This prevents your controller from constantly polling the API server.

### 2. The Lister (The Reader)
*   **What it is:** A read-only interface to the Informer's local cache.
*   **Minimum Requirement:** Usually paired 1:1 with an Informer.
*   **How it works:** When the controller logic needs to inspect a resource, it uses the Lister to read it from local memory instead of calling the API server.

### 3. The Workqueue (The TODO List)
*   **What it is:** A queue that holds the work to be processed (usually just the resource's key, e.g., `"namespace/name"`).
*   **Minimum Requirement:** One queue per controller.
*   **Why we need it:**
    *   **Rate Limiting:** Allows retrying with exponential backoff on failure.
    *   **De-duplication:** Coalesces multiple rapid changes into a single queued item.
    *   **Concurrency Safety:** Ensures one object isn't processed by multiple workers at once.

### 4. Event Handlers (The Glue)
*   **What it is:** Callback functions registered with the Informer.
*   **How it works:** Triggers on Add, Update, or Delete events, extracts the resource key, and puts it into the Workqueue.

### 5. The Worker Loop / Reconciler (The Doer)
*   **What it is:** A loop running in a background goroutine.
*   **How it works:** Pulls a key from the Workqueue, reads the resource from the Cache via the Lister, and executes business logic to make the current state match the desired state.

## The Big Picture Flow
Here is how an event flows through these components:
1.  User creates a resource -> 2. API Server notifies Informer -> 3. Informer updates Cache -> 4. Informer triggers Event Handler -> 5. Event Handler queues key -> 6. Worker pulls key -> 7. Worker reads from Cache via Lister -> 8. Worker executes business logic.

## Comparison to `controller-runtime`
Comparing this low-level approach to **`controller-runtime`** (used by Kubebuilder):
*   In `controller-runtime`, components 1 through 7 are managed for you. You only write the `Reconcile` function (step 8).
*   In direct `client-go`, you must write the boilerplate to set up all these components yourself, which yields more control at the cost of more code.
