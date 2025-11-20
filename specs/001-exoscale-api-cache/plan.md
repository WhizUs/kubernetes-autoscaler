# Implementation Plan: Exoscale API Caching Layer

**Branch**: `001-exoscale-api-cache` | **Date**: 2025-11-17 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/001-exoscale-api-cache/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Implement a caching layer for Exoscale API calls to reduce latency and API load. The system will cache instances (5min), instance pools (5min), SKS clusters (10min), and quota data (1hr) with proportional jitter (10-30% of TTL) to prevent request storms. The implementation uses an in-memory, process-local cache with `sync.RWMutex` for thread-safety, following patterns from Azure and AWS providers. Consecutive autoscaler loops will use cached data without additional API calls when cache is valid.

## Technical Context

**Language/Version**: Go 1.24.0
**Primary Dependencies**:
  - `k8s.io/autoscaler/cluster-autoscaler/cloudprovider` (provider interfaces)
  - `k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2` (Exoscale SDK, vendored)
  - Standard library: `sync`, `time`, `context`

**Storage**: In-memory cache (no persistence required)
**Testing**: Go standard testing + testify/suite + testify/mock
**Target Platform**: Linux server (Kubernetes cluster-autoscaler)
**Project Type**: Single project (provider plugin within cluster-autoscaler)
**Performance Goals**:
  - Reduce API call latency by 50% (cache hits vs API calls)
  - Achieve 85%+ cache hit rate during steady-state
  - Reduce API calls by 90% during normal operation

**Constraints**:
  - Must remain within `./cluster-autoscaler/cloudprovider/exoscale/` directory
  - Cache TTL fixed at runtime (not dynamically configurable)
  - Thread-safe for concurrent autoscaler loops
  - No external dependencies beyond existing provider dependencies

**Scale/Scope**:
  - Typical clusters: 10-100 nodes
  - Cache 4 resource types (instances, pools, clusters, quota)
  - Autoscaler loop frequency: 10-30 seconds
  - Expected API call reduction: thousands of calls/day to hundreds

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

### ✅ I. Minimal Architectural Changes
**Status**: PASS
- Cache implementation is purely additive within Exoscale provider
- No changes to cluster-autoscaler core architecture
- No modifications to provider interfaces or contracts

### ✅ II. Cross-Provider Pattern Alignment
**Status**: PASS - Providers surveyed: AWS, Azure, GCE
- **AWS pattern**: TTL-based cache with jitter (instance_type_cache.go, managed_nodegroup_cache.go)
- **Azure pattern**: Time-interval refresh with mutex (azure_cache.go, azure_scale_set_instance_cache.go)
- **GCE pattern**: Map-based cache with explicit invalidation (cache.go)
- **Adopted approach**: Hybrid of Azure (time-based TTL) + AWS (jitter mechanism)
- **Rationale**: Azure's pattern fits Exoscale's refresh-based architecture; AWS jitter prevents request storms

### ✅ III. Dependency Reuse
**Status**: PASS - No new dependencies required
- Thread-safety: `sync.RWMutex` (standard library, used by all surveyed providers)
- Time/TTL: `time.Time`, `time.Duration` (standard library)
- Jitter: `math/rand` (standard library, same as AWS provider)
- Cache storage: Go maps (standard library, used by Azure and GCE)

### ✅ IV. Comprehensive Testing
**Status**: PASS - Test plan defined
- Unit tests for cache operations (Get, Set, Expiration, Jitter)
- Integration tests with mock Exoscale client (using testify/mock pattern)
- Edge case tests (stale data serving, concurrent access, cache misses)
- Contract tests (cache transparency to existing code)

### ✅ V. Provider Isolation
**Status**: PASS
- All changes in `./cluster-autoscaler/cloudprovider/exoscale/`
- New files: `exoscale_cache.go`, `exoscale_cache_test.go`
- Modified files: `exoscale_manager.go` (wrap client with cache layer)
- No changes outside provider boundary

**GATE RESULT**: ✅ ALL CHECKS PASS - Proceed to Phase 0

## Project Structure

### Documentation (this feature)

```text
specs/001-exoscale-api-cache/
├── spec.md              # Feature specification (existing)
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
cluster-autoscaler/cloudprovider/exoscale/
├── exoscale_cache.go                    # NEW: Cache implementation
├── exoscale_cache_test.go               # NEW: Cache unit tests
├── exoscale_manager.go                  # MODIFIED: Wrap client with cache
├── exoscale_manager_test.go             # MODIFIED: Add cache integration tests
├── exoscale_node_group_instance_pool.go # MODIFIED: Use cached client
├── exoscale_node_group_sks_nodepool.go  # MODIFIED: Use cached client
├── exoscale_cloud_provider.go           # MODIFIED: Use cached client
└── exoscale_cloud_provider_test.go      # MODIFIED: Update tests for cache
```

**Structure Decision**: Single provider plugin structure (Option 1 pattern). The cache is implemented as a new component within the existing Exoscale provider. The cache wraps the existing `exoscaleClient` interface, making it transparent to existing code. Tests follow the established pattern using testify/mock.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| [e.g., 4th project] | [current need] | [why 3 projects insufficient] |
| [e.g., Repository pattern] | [specific problem] | [why direct DB access insufficient] |
