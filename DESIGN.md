# Project Documentation

When working on this project, please refer to the following design documents for context.
When you encounter these Google Doc links, please use appropriate tools (such as the `gdocs` skill if available) to read them. Please read the main tab of the document unless otherwise instructed.

*   [TPUs on Self-managed Kubernetes](https://docs.google.com/document/d/1WdWFimPUNYr2P8veR8f6wplshVVrZ2pVkBY5rGkp9RQ/edit)
    *   Describes the core design of the `TPUNodeGroup` controller, including CRDs (`TPUNodeGroup`, `TPUNodeState`) and state tracking conditions.
*   [TPU on self-managed clusters - Moving to Composite Patterns](https://docs.google.com/document/d/1L6OYG4y-4qH9fosPNuuDJQMhIk-WrAe-NDpbdC7keC8/edit)
    *   Proposes a redesign from a monolithic to a composite pattern (Upper/Lower layers) and explains the decision to build custom controllers instead of using KCC.
