# Data Model: Exoscale API Caching Layer

**Feature**: 001-exoscale-api-cache
**Date**: 2025-11-17
**Status**: Complete

## Overview

This document defines the data structures for the Exoscale API caching layer. The cache stores four resource types with associated metadata for TTL management, metrics tracking, and graceful degradation.

## Core Entities

### 1. exoscaleCache

Primary cache structure implementing the `exoscaleClient` interface.

```go
type exoscaleCache struct {
    // Wrapped client
    client exoscaleClient

    // Cache storage - keyed by resource ID
    instancesCache     map[string]*cachedInstance
    instancePoolsCache map[string]*cachedInstancePool
    sksClustersCache   []*cachedSKSCluster  // List operation result
    quotaCache         map[string]*cachedQuota  // Keyed by resource type

    // Thread-safety
    mu sync.RWMutex

    // Configuration
    jitterEnabled bool

    // Metrics
    metrics cacheMetrics
}
```

**Fields**:
- `client`: Underlying Exoscale API client (egoscale.Client)
- `instancesCache`: Cached compute instances, keyed by instance ID
- `instancePoolsCache`: Cached instance pools, keyed by pool ID
- `sksClustersCache`: Cached SKS clusters (full list result)
- `quotaCache`: Cached quota data, keyed by resource type (e.g., "instance")
- `mu`: Read-write mutex for thread-safe access
- `jitterEnabled`: Feature flag for jitter (default: true)
- `metrics`: Per-resource-type hit/miss counters

**Relationships**:
- Implements `exoscaleClient` interface (wraps actual client)
- Contains multiple `cached*` structs for different resource types
- Aggregates `cacheMetrics` for observability

**Validation Rules**:
- Cache keys must be non-empty strings
- Cached entries must have valid `fetchedAt` timestamps
- TTL must be positive duration

### 2. cachedInstance

Cache entry for Exoscale compute instances (5-minute TTL).

```go
type cachedInstance struct {
    data      *egoscale.Instance
    fetchedAt time.Time
    ttl       time.Duration
}
```

**Fields**:
- `data`: Actual instance data from Exoscale API
- `fetchedAt`: Timestamp when data was fetched
- `ttl`: Time-to-live duration (5 minutes + jitter)

**Methods**:
```go
func (c *cachedInstance) isExpired() bool {
    return time.Since(c.fetchedAt) > c.ttl
}
```

**Validation Rules**:
- `data` must not be nil
- `fetchedAt` must be in the past
- `ttl` must be positive (base: 5 minutes, with jitter: 5-6.5 minutes)

**State Transitions**:
1. **Fresh**: `!isExpired()` - Data is valid and can be served
2. **Expired**: `isExpired()` - Data needs refresh
3. **Stale**: Expired but served during API failure (TTL extended)

### 3. cachedInstancePool

Cache entry for Exoscale instance pools (5-minute TTL).

```go
type cachedInstancePool struct {
    data      *egoscale.InstancePool
    fetchedAt time.Time
    ttl       time.Duration
}
```

**Fields**:
- `data`: Actual instance pool data from Exoscale API
- `fetchedAt`: Timestamp when data was fetched
- `ttl`: Time-to-live duration (5 minutes + jitter)

**Methods**:
```go
func (c *cachedInstancePool) isExpired() bool {
    return time.Since(c.fetchedAt) > c.ttl
}
```

**Validation Rules**:
- Same as `cachedInstance`
- Base TTL: 5 minutes, with jitter: 5-6.5 minutes

**State Transitions**: Same as `cachedInstance`

### 4. cachedSKSCluster

Cache entry for Exoscale SKS clusters (10-minute TTL).

```go
type cachedSKSCluster struct {
    data      *egoscale.SKSCluster
    fetchedAt time.Time
    ttl       time.Duration
}
```

**Fields**:
- `data`: Actual SKS cluster data from Exoscale API
- `fetchedAt`: Timestamp when data was fetched
- `ttl`: Time-to-live duration (10 minutes + jitter)

**Methods**:
```go
func (c *cachedSKSCluster) isExpired() bool {
    return time.Since(c.fetchedAt) > c.ttl
}
```

**Validation Rules**:
- Same as `cachedInstance`
- Base TTL: 10 minutes, with jitter: 10-13 minutes

**State Transitions**: Same as `cachedInstance`

**Note**: SKS clusters are cached as a list (result of `ListSKSClusters()`) rather than individual entries.

### 5. cachedQuota

Cache entry for Exoscale quota data (1-hour TTL).

```go
type cachedQuota struct {
    data      *egoscale.Quota
    fetchedAt time.Time
    ttl       time.Duration
}
```

**Fields**:
- `data`: Actual quota data from Exoscale API
- `fetchedAt`: Timestamp when data was fetched
- `ttl`: Time-to-live duration (1 hour + jitter)

**Methods**:
```go
func (c *cachedQuota) isExpired() bool {
    return time.Since(c.fetchedAt) > c.ttl
}
```

**Validation Rules**:
- Same as `cachedInstance`
- Base TTL: 1 hour, with jitter: 60-78 minutes

**State Transitions**: Same as `cachedInstance`

### 6. cacheMetrics

Observability metrics for cache performance.

```go
type cacheMetrics struct {
    instanceHits       int64
    instanceMisses     int64
    instancePoolHits   int64
    instancePoolMisses int64
    sksClusterHits     int64
    sksClusterMisses   int64
    quotaHits          int64
    quotaMisses        int64
}
```

**Fields**:
- Per-resource-type hit counters (incremented on cache hits)
- Per-resource-type miss counters (incremented on cache misses)

**Methods**:
```go
func (m *cacheMetrics) hitRate(hits, misses int64) float64 {
    total := hits + misses
    if total == 0 {
        return 0.0
    }
    return float64(hits) / float64(total)
}
```

**Validation Rules**:
- Counters must be non-negative
- Counters use atomic operations for thread-safety

**Note**: Counters are updated using `atomic.AddInt64()` to avoid mutex overhead.

## Cache Entry Lifecycle

```
┌─────────────┐
│   Empty     │ Initial state (no data)
└──────┬──────┘
       │ API call (cache miss)
       ↓
┌─────────────┐
│   Fresh     │ Data valid, within TTL
└──────┬──────┘
       │ Time passes > TTL
       ↓
┌─────────────┐
│  Expired    │ Data stale, needs refresh
└──────┬──────┘
       │
       ├─→ API success: Return to Fresh with new data
       │
       └─→ API failure: Transition to Stale
           ↓
       ┌─────────────┐
       │   Stale     │ Expired data served during API failure
       └──────┬──────┘
              │ TTL extended
              └─→ Retry API call next cycle
```

## TTL Configuration

| Resource Type    | Base TTL | Jitter Range | Final TTL Range     |
|------------------|----------|--------------|---------------------|
| Instance         | 5 min    | 10-30%       | 5:00 - 6:30 min     |
| Instance Pool    | 5 min    | 10-30%       | 5:00 - 6:30 min     |
| SKS Cluster      | 10 min   | 10-30%       | 10:00 - 13:00 min   |
| Quota            | 1 hour   | 10-30%       | 60:00 - 78:00 min   |

**Jitter Formula**:
```go
func calculateTTLWithJitter(baseTTL time.Duration, jitterEnabled bool) time.Duration {
    if !jitterEnabled {
        return baseTTL
    }
    // Random jitter between 10% and 30%
    jitterPercent := 10 + rand.Intn(21)  // [10, 30]
    jitter := time.Duration(float64(baseTTL) * float64(jitterPercent) / 100.0)
    return baseTTL + jitter
}
```

## Thread-Safety Model

**Read Operations** (cache hits):
- Acquire `RLock()` for concurrent read access
- Multiple goroutines can read simultaneously
- No blocking on read-heavy workloads

**Write Operations** (cache updates):
- Acquire `Lock()` for exclusive write access
- Blocks all other operations during write
- Double-check pattern after acquiring lock to avoid race conditions

**Metrics Updates**:
- Use `atomic.AddInt64()` to avoid mutex overhead
- Lock-free increments for hit/miss counters

**Example Pattern**:
```go
// Read operation
c.mu.RLock()
cached, ok := c.instancesCache[id]
c.mu.RUnlock()

// Write operation (after cache miss)
c.mu.Lock()
defer c.mu.Unlock()

// Double-check after acquiring write lock
if cached, ok := c.instancesCache[id]; ok && !cached.isExpired() {
    return cached.data, nil
}

// Perform API call and update cache
data, err := c.client.GetInstance(ctx, zone, id)
if err == nil {
    c.instancesCache[id] = &cachedInstance{
        data:      data,
        fetchedAt: time.Now(),
        ttl:       calculateTTLWithJitter(5*time.Minute, c.jitterEnabled),
    }
}
```

## Memory Considerations

**Estimated Memory Usage**:
- Instance cache: ~1 KB per instance × 100 instances = 100 KB
- Instance pool cache: ~500 bytes per pool × 10 pools = 5 KB
- SKS cluster cache: ~2 KB per cluster × 5 clusters = 10 KB
- Quota cache: ~200 bytes × 5 resource types = 1 KB

**Total**: ~116 KB for typical cluster (negligible overhead)

**Cleanup Strategy**: No explicit cleanup needed
- Cache entries naturally expire and are replaced
- Maps are bounded by actual resource count in Exoscale account
- No unbounded growth expected

---

**Data Model Complete**: All entities defined with validation rules and relationships. Ready for contract generation.
