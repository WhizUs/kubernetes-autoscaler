# Implementation Quickstart: Exoscale API Caching Layer

**Feature**: 001-exoscale-api-cache
**Date**: 2025-11-17
**For**: Developers implementing this feature

## Overview

This guide provides a step-by-step implementation path for the Exoscale API caching layer. Follow these steps in order to build the feature incrementally with tests at each stage.

## Prerequisites

- Go 1.24.0 installed
- Cluster-autoscaler repository cloned
- Familiarity with testify/mock testing framework
- Understanding of Go interfaces and sync primitives

## Implementation Order

### Phase 1: Core Cache Structure (Day 1)

**File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache.go`

**Steps**:
1. Define cache entry structs (`cachedInstance`, `cachedInstancePool`, etc.)
2. Implement `isExpired()` method for cache entries
3. Define `exoscaleCache` struct with maps and mutex
4. Implement constructor `newExoscaleCache(client, jitterEnabled)`
5. Implement jitter calculation function

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache_test.go`

**Tests**:
- Test `isExpired()` with fresh and stale data
- Test jitter calculation ranges (10-30%)
- Test cache initialization

**Example**:
```go
func TestCacheEntryExpiration(t *testing.T) {
    entry := &cachedInstance{
        data:      &egoscale.Instance{},
        fetchedAt: time.Now().Add(-10 * time.Minute),
        ttl:       5 * time.Minute,
    }
    assert.True(t, entry.isExpired())
}
```

**Checkpoint**: Cache structure compiles and basic tests pass

---

### Phase 2: GetInstance with Caching (Day 1-2)

**File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache.go`

**Steps**:
1. Implement `GetInstance()` method
2. Add cache hit path (read lock, check expiration, return)
3. Add cache miss path (write lock, API call, store)
4. Add stale data serving on API failure
5. Add metrics tracking (atomic counters)

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache_test.go`

**Tests**:
- Test cache hit returns cached data without API call
- Test cache miss calls API and stores result
- Test expired entry refreshes from API
- Test API failure serves stale data
- Test API failure without cache returns error
- Test metrics counters increment correctly

**Example**:
```go
func TestGetInstance_CacheHit(t *testing.T) {
    mockClient := new(exoscaleClientMock)
    cache := newExoscaleCache(mockClient, false)

    // Pre-populate cache
    cache.instancesCache["test-id"] = &cachedInstance{
        data:      &egoscale.Instance{ID: ptr("test-id")},
        fetchedAt: time.Now(),
        ttl:       5 * time.Minute,
    }

    // Should not call API
    instance, err := cache.GetInstance(context.Background(), "ch-gva-2", "test-id")

    assert.NoError(t, err)
    assert.Equal(t, "test-id", *instance.ID)
    mockClient.AssertNotCalled(t, "GetInstance")
    assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceHits))
}
```

**Checkpoint**: GetInstance works with caching, all tests pass

---

### Phase 3: Remaining Cache Methods (Day 2)

**File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache.go`

**Steps**:
1. Implement `GetInstancePool()` (same pattern as GetInstance)
2. Implement `ListSKSClusters()` (cache entire list)
3. Implement `GetQuota()` (same pattern, different TTL)
4. Implement pass-through methods (Evict*, Scale*)

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache_test.go`

**Tests**:
- Test each cached method with hit/miss scenarios
- Test ListSKSClusters caches full list
- Test pass-through methods always call API
- Test different TTLs for different resource types

**Checkpoint**: All exoscaleClient interface methods implemented

---

### Phase 4: Observability & Management (Day 2-3)

**File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache.go`

**Steps**:
1. Implement `GetMetrics()` method
2. Implement hit rate calculations
3. Implement `InvalidateCache()` method
4. Implement `SetJitterEnabled()` method

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache_test.go`

**Tests**:
- Test GetMetrics returns correct hit rates
- Test InvalidateCache clears all entries
- Test SetJitterEnabled affects TTL calculations

**Example**:
```go
func TestGetMetrics_HitRate(t *testing.T) {
    cache := newExoscaleCache(nil, false)
    atomic.StoreInt64(&cache.metrics.instanceHits, 85)
    atomic.StoreInt64(&cache.metrics.instanceMisses, 15)

    metrics := cache.GetMetrics()

    assert.Equal(t, 0.85, metrics.InstanceHitRate)
}
```

**Checkpoint**: Cache is fully functional and observable

---

### Phase 5: Integration with Manager (Day 3)

**File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_manager.go`

**Steps**:
1. Modify `NewManager()` to wrap client with cache
2. Add cache configuration (jitter enabled by default)
3. No changes to existing method calls (interface compatibility)

**Before**:
```go
func NewManager(...) *Manager {
    client := egoscale.NewClient(...)
    return &Manager{
        client: client,
        // ...
    }
}
```

**After**:
```go
func NewManager(...) *Manager {
    client := egoscale.NewClient(...)
    cachedClient := newExoscaleCache(client, true)  // Enable jitter
    return &Manager{
        client: cachedClient,  // Now cached
        // ...
    }
}
```

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_manager_test.go`

**Tests**:
- Test Manager uses cached client
- Test end-to-end scenarios with cache
- Test cache reduces API call count

**Checkpoint**: Manager integration complete

---

### Phase 6: Thread-Safety Testing (Day 3-4)

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cache_test.go`

**Tests**:
- Test concurrent reads (multiple goroutines reading same ID)
- Test concurrent writes (race detector enabled)
- Test read during write
- Test metrics updates are atomic

**Example**:
```go
func TestConcurrentReads(t *testing.T) {
    mockClient := new(exoscaleClientMock)
    cache := newExoscaleCache(mockClient, false)

    // Pre-populate
    cache.instancesCache["test-id"] = &cachedInstance{
        data:      &egoscale.Instance{ID: ptr("test-id")},
        fetchedAt: time.Now(),
        ttl:       5 * time.Minute,
    }

    // 100 concurrent reads
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, err := cache.GetInstance(context.Background(), "ch-gva-2", "test-id")
            assert.NoError(t, err)
        }()
    }
    wg.Wait()

    // All should hit cache
    mockClient.AssertNotCalled(t, "GetInstance")
}
```

**Run with race detector**:
```bash
cd cluster-autoscaler/cloudprovider/exoscale
go test -race -v
```

**Checkpoint**: No race conditions, thread-safe operation confirmed

---

### Phase 7: Integration Testing (Day 4)

**Test File**: `cluster-autoscaler/cloudprovider/exoscale/exoscale_cloud_provider_test.go`

**Steps**:
1. Update existing tests to work with cached client
2. Add integration tests for common workflows
3. Test cache behavior during node refresh cycles

**Tests**:
- Test NodeGroupForNode with cached SKS clusters
- Test Nodes() method uses cached instances
- Test Refresh() benefits from cached pools
- Test consecutive loops use cache (no API calls)

**Example**:
```go
func (suite *cloudProviderTestSuite) TestRefresh_UsesCache() {
    // First refresh - populates cache
    err := suite.provider.Refresh()
    suite.NoError(err)

    // Mock should be called once
    suite.client.AssertNumberOfCalls(suite.T(), "GetInstancePool", 1)

    // Second refresh within TTL - uses cache
    err = suite.provider.Refresh()
    suite.NoError(err)

    // Still only one API call
    suite.client.AssertNumberOfCalls(suite.T(), "GetInstancePool", 1)
}
```

**Checkpoint**: Integration tests pass, cache works in realistic scenarios

---

### Phase 8: Documentation & Cleanup (Day 4-5)

**Files**:
- Add godoc comments to all exported functions
- Update `cluster-autoscaler/cloudprovider/exoscale/README.md` with cache information
- Add inline comments for complex logic

**Documentation**:
```go
// newExoscaleCache creates a new caching layer that wraps an exoscaleClient.
// The cache implements the exoscaleClient interface, making it a drop-in replacement.
//
// Cache TTLs:
//   - Instances: 5 minutes
//   - Instance Pools: 5 minutes
//   - SKS Clusters: 10 minutes
//   - Quota: 1 hour
//
// Thread-safety: All cache operations are thread-safe using sync.RWMutex.
// Jitter: When enabled, adds 10-30% random jitter to TTLs to prevent thundering herd.
//
// Graceful degradation: On API failures, serves stale cached data if available.
func newExoscaleCache(client exoscaleClient, jitterEnabled bool) *exoscaleCache {
    // ...
}
```

**Checkpoint**: Code is well-documented and maintainable

---

## Testing Checklist

Before considering implementation complete:

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Race detector shows no issues (`go test -race`)
- [ ] Test coverage > 80% for cache code
- [ ] Mock client never called on cache hits in tests
- [ ] Metrics accurately track hits/misses
- [ ] Stale data serving works correctly
- [ ] Jitter produces values in 10-30% range
- [ ] Thread-safety verified under concurrent load
- [ ] Existing autoscaler functionality unchanged

## Running Tests

```bash
# Unit tests
cd cluster-autoscaler/cloudprovider/exoscale
go test -v

# With race detector
go test -race -v

# With coverage
go test -cover -v

# Specific test
go test -v -run TestGetInstance_CacheHit
```

## Common Pitfalls

1. **Forgetting double-check pattern**: After acquiring write lock, always check cache again
2. **Not releasing locks**: Use `defer` for all mutex operations
3. **Caching write operations**: Don't cache Evict*/Scale* methods
4. **Nil pointer dereference**: Check cached data isn't nil before using
5. **Race conditions in metrics**: Use atomic operations for counters
6. **Incorrect jitter math**: Ensure jitter is added to base TTL, not multiplied
7. **Not testing API failures**: Must test stale data serving

## Performance Validation

After implementation, validate performance improvements:

```bash
# Run autoscaler with cache
# Monitor metrics for:
# - Cache hit rate > 85%
# - API call reduction > 90%
# - Latency reduction > 50%
```

## Debugging Tips

**Enable verbose logging**:
```go
import "k8s.io/klog/v2"

klog.V(3).Infof("Cache hit for instance %s", id)
klog.V(3).Infof("Cache miss for instance %s, fetching from API", id)
klog.Warningf("API call failed, serving stale data for %s: %v", id, err)
```

**Check cache state in tests**:
```go
// Inspect cache after operation
t.Logf("Cache size: %d", len(cache.instancesCache))
t.Logf("Metrics: %+v", cache.GetMetrics())
```

---

**Implementation Complete**: Follow these phases to build a robust, tested caching layer.
