# Project Scope Definition

## Repository Structure

This repository contains the Kubernetes autoscaler project, which includes multiple subprojects:
- `cluster-autoscaler/` - Main cluster autoscaler implementation
- `vertical-pod-autoscaler/` - Vertical pod autoscaler
- `addon-resizer/` - Addon resizer
- Other autoscaler-related components

## Work Scope Constraint

**IMPORTANT**: All development work in this repository is **strictly limited to the `cluster-autoscaler/` subdirectory**.

### What This Means

- All code changes MUST be within `./cluster-autoscaler/`
- All file reads and searches should focus on `./cluster-autoscaler/`
- When referencing paths in documentation, use `./cluster-autoscaler/` prefix
- No modifications to other subprojects (vertical-pod-autoscaler, addon-resizer, etc.)

### Rationale

The cluster-autoscaler is being developed independently from other subprojects in this repository. This constraint:
- Prevents accidental changes to unrelated components
- Maintains clear boundaries between subprojects
- Simplifies testing and code review
- Reduces risk of breaking changes to other components

### Exoscale Provider Scope

Within the cluster-autoscaler, work is further restricted to:
- Primary directory: `./cluster-autoscaler/cloudprovider/exoscale/`
- Tests: `./cluster-autoscaler/cloudprovider/exoscale/*_test.go`
- Documentation: `./cluster-autoscaler/cloudprovider/exoscale/README.md`

See [constitution.md](./constitution.md) for detailed development principles and constraints specific to the Exoscale provider.

---

**Version**: 1.0.0 | **Created**: 2025-11-17
