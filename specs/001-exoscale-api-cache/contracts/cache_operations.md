# Cache Operations Contract

**Feature**: 001-exoscale-api-cache
**Date**: 2025-11-17
**Purpose**: Define the behavioral contract for cache operations

## Operation: GetInstance (Cached Read)

### Signature
```go
GetInstance(ctx context.Context, zone string, id string) (*egoscale.Instance, error)
```

### Scenarios

#### Scenario 1: Cache Hit (Fresh Data)
**Preconditions**:
- Instance with ID exists in cache
- Cache entry not expired (`time.Since(fetchedAt) <= ttl`)

**Input**:
```go
ctx := context.Background()
zone := "ch-gva-2"
id := "instance-abc-123"
```

**Process**:
1. Acquire read lock (`RLock()`)
2. Check cache for instance ID
3. Validate expiration
4. Release read lock (`RUnlock()`)
5. Increment hit counter (atomic)
6. Return cached data

**Output**:
```go
instance := &egoscale.Instance{...}  // From cache
err := nil
```

**Postconditions**:
- No API call made
- `metrics.instanceHits` incremented by 1
- Cache unchanged

**Performance**: < 1 μs (in-memory map lookup)

---

#### Scenario 2: Cache Miss (No Data)
**Preconditions**:
- Instance with ID NOT in cache

**Input**:
```go
ctx := context.Background()
zone := "ch-gva-2"
id := "instance-xyz-789"
```

**Process**:
1. Acquire read lock (`RLock()`)
2. Check cache - not found
3. Release read lock (`RUnlock()`)
4. Acquire write lock (`Lock()`)
5. Double-check cache (race condition protection)
6. Call API: `client.GetInstance(ctx, zone, id)`
7. Store result in cache with TTL + jitter
8. Release write lock (`Unlock()`)
9. Increment miss counter (atomic)
10. Return fresh data

**Output**:
```go
instance := &egoscale.Instance{...}  // From API
err := nil  // Or error from API
```

**Postconditions**:
- API call made
- `metrics.instanceMisses` incremented by 1
- Cache populated with new entry
- Entry TTL = 5min + (10-30% jitter)

**Performance**: ~50-200ms (network API call)

---

#### Scenario 3: Cache Expired (Stale Data)
**Preconditions**:
- Instance with ID exists in cache
- Cache entry expired (`time.Since(fetchedAt) > ttl`)

**Input**:
```go
ctx := context.Background()
zone := "ch-gva-2"
id := "instance-old-456"
```

**Process**:
1. Acquire read lock (`RLock()`)
2. Check cache - found but expired
3. Release read lock (`RUnlock()`)
4. Acquire write lock (`Lock()`)
5. Double-check expiration
6. Call API: `client.GetInstance(ctx, zone, id)`
7. Update cache with fresh data
8. Release write lock (`Unlock()`)
9. Increment miss counter (atomic)
10. Return fresh data

**Output**:
```go
instance := &egoscale.Instance{...}  // From API (refreshed)
err := nil
```

**Postconditions**:
- API call made
- `metrics.instanceMisses` incremented by 1
- Cache refreshed with new timestamp and TTL
- Old data replaced

**Performance**: ~50-200ms (network API call)

---

#### Scenario 4: API Failure - Serve Stale Data (Graceful Degradation)
**Preconditions**:
- Instance with ID exists in cache (expired)
- API call fails (network error, rate limit, service unavailable)

**Input**:
```go
ctx := context.Background()
zone := "ch-gva-2"
id := "instance-stale-999"
```

**Process**:
1. Acquire read lock (`RLock()`)
2. Check cache - found but expired
3. Release read lock (`RUnlock()`)
4. Acquire write lock (`Lock()`)
5. Call API: `client.GetInstance(ctx, zone, id)`
6. API returns error
7. Check if stale data available in cache
8. Log warning about serving stale data
9. Extend TTL on stale entry (add another cycle)
10. Release write lock (`Unlock()`)
11. Increment miss counter (atomic)
12. Return stale data

**Output**:
```go
instance := &egoscale.Instance{...}  // From cache (stale)
err := nil  // Error suppressed for graceful degradation
```

**Postconditions**:
- API call attempted and failed
- `metrics.instanceMisses` incremented by 1
- Warning logged
- Cache TTL extended by base TTL
- Stale data served

**Performance**: ~50-200ms (failed API call) + < 1 μs (cache access)

---

#### Scenario 5: API Failure - No Cache Data
**Preconditions**:
- Instance with ID NOT in cache
- API call fails

**Input**:
```go
ctx := context.Background()
zone := "ch-gva-2"
id := "instance-new-000"
```

**Process**:
1. Acquire read lock (`RLock()`)
2. Check cache - not found
3. Release read lock (`RUnlock()`)
4. Acquire write lock (`Lock()`)
5. Call API: `client.GetInstance(ctx, zone, id)`
6. API returns error
7. No stale data available
8. Release write lock (`Unlock()`)
9. Increment miss counter (atomic)
10. Return error

**Output**:
```go
instance := nil
err := fmt.Errorf("failed to get instance: %w", apiErr)
```

**Postconditions**:
- API call attempted and failed
- `metrics.instanceMisses` incremented by 1
- No cache entry created
- Error propagated to caller

**Performance**: ~50-200ms (failed API call)

---

## Operation: GetInstancePool (Cached Read)

Same contract as `GetInstance` with different TTL (5 minutes) and resource type.

## Operation: ListSKSClusters (Cached List)

### Signature
```go
ListSKSClusters(ctx context.Context, zone string) ([]*egoscale.SKSCluster, error)
```

### Special Considerations

**Difference from other operations**:
- Returns full list (not individual item)
- Cache stores entire list result
- Single cache entry for all clusters in zone

**Cache Key**: Zone name (e.g., "ch-gva-2")

**TTL**: 10 minutes + jitter

**Expiration**: Entire list expires together

---

## Operation: GetQuota (Cached Read)

Same contract as `GetInstance` with different TTL (1 hour) and cache key (resource type, e.g., "instance").

---

## Operation: GetMetrics (Observability)

### Signature
```go
GetMetrics() CacheMetrics
```

### Behavior
**Preconditions**: None

**Process**:
1. Read hit counters atomically
2. Read miss counters atomically
3. Calculate hit rates
4. Return metrics struct

**Output**:
```go
CacheMetrics{
    InstanceHitRate:     0.87,
    InstancePoolHitRate: 0.92,
    SKSClusterHitRate:   0.95,
    QuotaHitRate:        0.99,
    TotalHits:           8543,
    TotalMisses:         1127,
    OverallHitRate:      0.88,
}
```

**Postconditions**: No state change

**Thread-Safety**: Atomic reads, no mutex needed

---

## Operation: InvalidateCache (Admin)

### Signature
```go
InvalidateCache()
```

### Behavior
**Preconditions**: None

**Process**:
1. Acquire write lock (`Lock()`)
2. Clear all cache maps
3. Reset metrics counters
4. Release write lock (`Unlock()`)

**Postconditions**:
- All cache entries removed
- Next access will trigger API calls
- Metrics reset to zero

**Use Case**: Testing, debugging, or forced refresh

---

## Write Operations (Pass-Through)

### Operations
- `EvictInstancePoolMembers()`
- `EvictSKSNodepoolMembers()`
- `ScaleInstancePool()`
- `ScaleSKSNodepool()`

### Contract
**Behavior**: Pass through to underlying client WITHOUT caching

**Rationale**:
- These are write operations (mutate state)
- Cached reads would become stale immediately
- No performance benefit from caching writes

**Postconditions**:
- API call always made
- No cache interaction
- No metrics tracking for write operations

---

## Thread-Safety Guarantees

### Concurrent Reads
**Scenario**: Multiple goroutines call `GetInstance()` for different IDs

**Behavior**: All reads proceed concurrently (RWMutex allows multiple readers)

**Performance**: No contention, linear scaling

---

### Concurrent Reads (Same ID)
**Scenario**: Multiple goroutines call `GetInstance("same-id")` simultaneously

**Behavior**: All reads proceed concurrently if data is cached

**Edge Case**: If cache miss occurs:
- First goroutine to acquire write lock fetches from API
- Other goroutines block temporarily
- After first goroutine completes, others see cached data

---

### Read During Write
**Scenario**: Goroutine A writes to cache, Goroutine B reads

**Behavior**: Goroutine B blocks until write completes

**Rationale**: RWMutex ensures consistency

---

### Concurrent Writes
**Scenario**: Multiple goroutines update cache simultaneously

**Behavior**: Writes serialized by mutex (only one at a time)

**Performance**: Minimal contention since cache hits don't write

---

## Error Handling

### API Errors
- **Network timeout**: Serve stale data if available, else propagate error
- **Rate limit (429)**: Serve stale data if available, else propagate error
- **Service unavailable (503)**: Serve stale data if available, else propagate error
- **Not found (404)**: Propagate error, do not cache
- **Unauthorized (401)**: Propagate error, do not cache

### Cache Errors
- **Nil data in cache**: Treat as cache miss, fetch from API
- **Corrupted timestamp**: Treat as expired, refresh from API

---

**Contract Complete**: All cache operations defined with preconditions, behavior, and postconditions.
