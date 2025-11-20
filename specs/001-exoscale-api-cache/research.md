# Research: Exoscale API Caching Layer

**Feature**: 001-exoscale-api-cache
**Date**: 2025-11-17
**Status**: Complete

## Overview

This document captures research findings for implementing a caching layer for Exoscale API calls in the cluster-autoscaler. Research focused on cross-provider caching patterns, thread-safety mechanisms, and integration approaches.

## Research Questions & Findings

### 1. Go Version and Tooling

**Decision**: Go 1.24.0
**Rationale**: Cluster-autoscaler uses Go 1.24.0 (cluster-autoscaler/go.mod:3)
**Alternatives considered**: N/A (must match project version)

**Evidence**:
- File: `cluster-autoscaler/go.mod`
- Line: 3 (`go 1.24.0`)

### 2. Testing Framework

**Decision**: Go standard testing + testify/suite + testify/mock
**Rationale**: Existing Exoscale provider uses this pattern extensively
**Alternatives considered**:
- mockery/gomock: Rejected because testify/mock is already used
- Standard testing only: Rejected because test suite pattern improves organization

**Evidence**:
- File: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cloud_provider_test.go`
- Lines: 19-31 (imports testify/suite and testify/mock)
- Lines: 120-124 (test suite pattern using `suite.Suite`)
- Lines: 54-118 (mock client implementation)

### 3. Cross-Provider Caching Patterns

**Decision**: Hybrid approach combining Azure's time-based TTL + AWS's jitter mechanism
**Rationale**:
- Azure's pattern fits Exoscale's refresh-based architecture
- AWS's jitter prevents thundering herd problems
- Both patterns use standard library (no new dependencies)

**Alternatives considered**:

#### A. AWS Pattern (k8s.io ExpirationStore)
- **Files**: `cluster-autoscaler/cloudprovider/aws/instance_type_cache.go`, `managed_nodegroup_cache.go`
- **Features**: TTL-based expiration, jitter clock, built-in thread-safety
- **TTLs**: 20min (instance types) with 2-10min jitter, 6min (managed nodegroups) with 5-60s jitter
- **Thread-safety**: Built into `cache.Store` from k8s.io/client-go
- **Jitter implementation**: Custom `jitterClock` with `sync.RWMutex` (Lines 48-61)
- **Rejected because**: Adds dependency on k8s.io/client-go/tools/cache; simpler to implement directly

#### B. Azure Pattern (Time-interval refresh)
- **Files**: `cluster-autoscaler/cloudprovider/azure/azure_cache.go`, `azure_scale_set_instance_cache.go`
- **Features**: Configurable refresh interval, mutex protection, rich metadata caching
- **TTLs**: 1-5min (configurable via env vars)
- **Thread-safety**: `sync.Mutex` for all cache operations
- **Cache structure**: Maps with metadata (Lines 58-107 in azure_cache.go)
- **Lazy validation**: Check expiration on access (Lines 90-98 in azure_scale_set_instance_cache.go)
- **Selected pattern**: Time-based validation with mutex

#### C. GCE Pattern (Map-based with explicit invalidation)
- **File**: `cluster-autoscaler/cloudprovider/gce/cache.go`
- **Features**: No TTL, explicit invalidation, single mutex for all caches
- **Thread-safety**: Single `sync.Mutex` (Line 62)
- **Cache structure**: Multiple maps for different resource types (Lines 62-81)
- **Rejected because**: No TTL requires explicit invalidation; doesn't fit autoscaler's periodic refresh pattern

**Selected Approach**:
```go
// Hybrid pattern structure
type exoscaleCache struct {
    client exoscaleClient  // Wrapped client

    // Cache storage (Azure pattern)
    instancesCache      map[string]*cachedInstance
    instancePoolsCache  map[string]*cachedInstancePool
    sksClustersCache    []*cachedSKSCluster
    quotaCache          map[string]*cachedQuota

    // Thread-safety (Azure pattern)
    mu sync.RWMutex

    // Jitter support (AWS pattern)
    jitterEnabled bool
}

// Cache entry with TTL (Azure pattern)
type cachedInstance struct {
    data      *egoscale.Instance
    fetchedAt time.Time
    ttl       time.Duration
}

// Jitter calculation (AWS pattern)
func (c *exoscaleCache) applyJitter(ttl time.Duration) time.Duration {
    if !c.jitterEnabled {
        return ttl
    }
    // 10-30% jitter as per spec
    jitterPercent := 10 + rand.Intn(21)  // 10-30
    jitter := time.Duration(float64(ttl) * float64(jitterPercent) / 100.0)
    return ttl + jitter
}
```

### 4. Thread-Safety Mechanisms

**Decision**: `sync.RWMutex` for read-heavy workloads
**Rationale**:
- Cache operations are predominantly reads (cache hits)
- RWMutex allows concurrent reads while protecting writes
- Used successfully in AWS jitter clock and can be applied to cache

**Alternatives considered**:
- `sync.Mutex`: Simpler but blocks all concurrent access (Azure uses this)
- Atomic operations: Too complex for map-based cache
- Channel-based synchronization: Unnecessary overhead

**Evidence**:
- AWS jitter clock uses `sync.RWMutex` (instance_type_cache.go:50-51)
- Azure uses `sync.Mutex` (azure_cache.go:58)
- GCE uses `sync.Mutex` (cache.go:62)

**Implementation pattern**:
```go
// Read operations (cache hits)
func (c *exoscaleCache) GetInstance(ctx context.Context, zone, id string) (*egoscale.Instance, error) {
    c.mu.RLock()
    cached, ok := c.instancesCache[id]
    c.mu.RUnlock()

    if ok && !cached.isExpired() {
        return cached.data, nil  // Cache hit
    }

    // Cache miss - acquire write lock
    c.mu.Lock()
    defer c.mu.Unlock()

    // Double-check after acquiring write lock
    if cached, ok := c.instancesCache[id]; ok && !cached.isExpired() {
        return cached.data, nil
    }

    // Fetch from API
    instance, err := c.client.GetInstance(ctx, zone, id)
    if err != nil {
        return nil, err
    }

    // Store in cache
    c.instancesCache[id] = &cachedInstance{
        data:      instance,
        fetchedAt: time.Now(),
        ttl:       5 * time.Minute,
    }

    return instance, nil
}
```

### 5. Exoscale Client Integration

**Decision**: Wrap existing `exoscaleClient` interface with cache layer
**Rationale**:
- Maintains interface compatibility
- No changes to calling code
- Easy to test with existing mock patterns

**Alternatives considered**:
- Modify manager directly: Rejected because it couples cache to manager logic
- Add cache as separate component: Rejected because it requires changes to all call sites

**Current client interface** (exoscale_manager.go:30-39):
```go
type exoscaleClient interface {
    EvictInstancePoolMembers(context.Context, string, *egoscale.InstancePool, []string) error
    EvictSKSNodepoolMembers(context.Context, string, *egoscale.SKSCluster, *egoscale.SKSNodepool, []string) error
    GetInstance(context.Context, string, string) (*egoscale.Instance, error)
    GetInstancePool(context.Context, string, string) (*egoscale.InstancePool, error)
    GetQuota(context.Context, string, string) (*egoscale.Quota, error)
    ListSKSClusters(context.Context, string) ([]*egoscale.SKSCluster, error)
    ScaleInstancePool(context.Context, string, *egoscale.InstancePool, int64) error
    ScaleSKSNodepool(context.Context, string, *egoscale.SKSCluster, *egoscale.SKSNodepool, int64) error
}
```

**Integration approach**:
```go
// Cache implements exoscaleClient interface
type exoscaleCache struct {
    client exoscaleClient  // Wrapped actual client
    // ... cache fields
}

// Manager initialization (modified)
func NewManager(...) *Manager {
    client := egoscale.NewClient(...)

    // Wrap client with cache
    cachedClient := newExoscaleCache(client, true /* jitter enabled */)

    return &Manager{
        client: cachedClient,  // Now using cached client
        // ... other fields
    }
}
```

### 6. Current API Call Patterns

**Finding**: Exoscale provider has NO caching currently - all calls go directly to API

**High-frequency call sites**:

1. **GetInstance** - Called for every node in every loop
   - `exoscale_cloud_provider.go:267` - In `instancePoolFromNode()`
   - `exoscale_node_group_instance_pool.go:167-171` - In `Nodes()` loop
   - `exoscale_node_group_sks_nodepool.go:182-190` - In SKS `Nodes()` loop

2. **GetInstancePool** - Called every refresh cycle (10s default)
   - `exoscale_manager.go:100` - In `Refresh()` method
   - `exoscale_node_group_instance_pool.go:218-231` - In `waitUntilRunning()` polling

3. **ListSKSClusters** - Full list operation for every SKS node
   - `exoscale_cloud_provider.go:80` - In `NodeGroupForNode()`

4. **GetQuota** - Called for size limit calculations
   - `exoscale_manager.go:120-127` - In `computeInstanceQuota()`

**Impact**: Without caching, a 50-node cluster with 10s refresh = ~300 GetInstance calls + 30 GetInstancePool calls per minute = ~19,800 API calls/hour for instances alone

### 7. Stale Data Handling

**Decision**: Serve stale data with extended TTL on API failure (graceful degradation)
**Rationale**: Autoscaler stability more important than perfect data freshness
**Alternatives considered**:
- Return error immediately: Rejected because it causes autoscaler to fail
- Retry with backoff: Rejected because spec requires serving stale data

**Implementation approach**:
```go
func (c *exoscaleCache) GetInstance(ctx context.Context, zone, id string) (*egoscale.Instance, error) {
    c.mu.RLock()
    cached, ok := c.instancesCache[id]
    c.mu.RUnlock()

    if ok && !cached.isExpired() {
        return cached.data, nil  // Fresh cache hit
    }

    c.mu.Lock()
    defer c.mu.Unlock()

    // Try to fetch from API
    instance, err := c.client.GetInstance(ctx, zone, id)
    if err != nil {
        // API failed - serve stale data if available
        if cached != nil {
            klog.Warningf("API call failed, serving stale instance data for %s: %v", id, err)
            // Extend TTL by another cycle
            cached.ttl = 5 * time.Minute
            cached.fetchedAt = time.Now()
            return cached.data, nil
        }
        return nil, err  // No stale data available
    }

    // Update cache with fresh data
    c.instancesCache[id] = &cachedInstance{
        data:      instance,
        fetchedAt: time.Now(),
        ttl:       5 * time.Minute,
    }

    return instance, nil
}
```

### 8. Cache Metrics

**Decision**: Track per-resource-type hit/miss counters
**Rationale**: Observability requirement from spec (FR-013)
**Alternatives considered**:
- No metrics: Rejected because spec requires observability
- Detailed metrics (latency, sizes): Deferred to future work

**Implementation approach**:
```go
type cacheMetrics struct {
    instanceHits         int64
    instanceMisses       int64
    instancePoolHits     int64
    instancePoolMisses   int64
    sksClusterHits       int64
    sksClusterMisses     int64
    quotaHits            int64
    quotaMisses          int64
}

func (c *exoscaleCache) recordHit(resourceType string) {
    switch resourceType {
    case "instance":
        atomic.AddInt64(&c.metrics.instanceHits, 1)
    // ... other types
    }
}

func (c *exoscaleCache) GetMetrics() map[string]float64 {
    return map[string]float64{
        "instance_hit_rate": float64(c.metrics.instanceHits) /
            float64(c.metrics.instanceHits + c.metrics.instanceMisses),
        // ... other rates
    }
}
```

## Summary

The implementation will use a hybrid caching approach:
- **Azure-style** time-based TTL with map storage
- **AWS-style** jitter mechanism (10-30% of TTL)
- **Thread-safety** via `sync.RWMutex` for read-heavy workloads
- **Interface wrapping** for transparent integration
- **Graceful degradation** serving stale data on API failures
- **Metrics tracking** for observability

This approach requires NO new dependencies, follows established patterns from 3 major providers (AWS, Azure, GCE), and integrates seamlessly with existing Exoscale provider code.

---

**Research Complete**: All NEEDS CLARIFICATION items resolved. Ready for Phase 1 (Design & Contracts).
