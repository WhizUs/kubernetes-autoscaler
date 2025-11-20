// Package contracts defines the interface contracts for the Exoscale API caching layer.
// This is a design artifact - NOT production code.
package contracts

import (
	"context"
	"time"

	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
)

// exoscaleClient is the interface that the cache wraps.
// This interface already exists in the actual codebase (exoscale_manager.go:30-39).
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

// CachedClient is the cache implementation that wraps exoscaleClient.
// It implements the same interface, making it a drop-in replacement.
type CachedClient interface {
	exoscaleClient

	// Additional methods for cache management and observability
	GetMetrics() CacheMetrics
	InvalidateCache()
	SetJitterEnabled(enabled bool)
}

// CacheMetrics provides observability into cache performance.
type CacheMetrics struct {
	InstanceHitRate       float64
	InstancePoolHitRate   float64
	SKSClusterHitRate     float64
	QuotaHitRate          float64
	TotalHits             int64
	TotalMisses           int64
	OverallHitRate        float64
}

// CacheEntry is the generic cache entry structure.
// This is NOT exported in production code - it's an internal detail.
type CacheEntry struct {
	FetchedAt time.Time
	TTL       time.Duration
}

// IsExpired checks if the cache entry has expired.
func (c *CacheEntry) IsExpired() bool {
	return time.Since(c.FetchedAt) > c.TTL
}

// Contract: Cache Behavior
//
// 1. Cache Hit (data is fresh):
//    - Input: GetInstance(ctx, zone, "instance-123")
//    - Precondition: Instance "instance-123" in cache, not expired
//    - Output: Cached instance data, no API call
//    - Postcondition: Hit counter incremented
//
// 2. Cache Miss (data not in cache):
//    - Input: GetInstance(ctx, zone, "instance-456")
//    - Precondition: Instance "instance-456" not in cache
//    - Output: Fresh data from API, stored in cache
//    - Postcondition: Miss counter incremented, cache populated
//
// 3. Cache Expired (data stale):
//    - Input: GetInstance(ctx, zone, "instance-789")
//    - Precondition: Instance "instance-789" in cache, expired
//    - Output: Fresh data from API, cache refreshed
//    - Postcondition: Miss counter incremented, cache updated
//
// 4. API Failure with Stale Data:
//    - Input: GetInstance(ctx, zone, "instance-999")
//    - Precondition: Instance "instance-999" in cache (expired), API call fails
//    - Output: Stale cached data with extended TTL
//    - Postcondition: Warning logged, cache TTL extended
//
// 5. API Failure without Cache:
//    - Input: GetInstance(ctx, zone, "instance-000")
//    - Precondition: Instance "instance-000" not in cache, API call fails
//    - Output: Error returned
//    - Postcondition: Miss counter incremented, no cache entry
//
// 6. Jitter Application:
//    - TTL = baseTTL + (baseTTL * jitterPercent / 100)
//    - jitterPercent in range [10, 30]
//    - Example: 5min base = 5:00 to 6:30 final TTL
//
// 7. Thread-Safety:
//    - Multiple concurrent GetInstance() calls: All reads allowed simultaneously
//    - Concurrent Get + Update: Reads blocked only during cache write
//    - Multiple Updates: Serialized via mutex
//
// 8. Metrics Atomicity:
//    - Hit/miss counters updated atomically (no mutex)
//    - GetMetrics() reads counters atomically
//    - No guaranteed consistency between different resource type counters
