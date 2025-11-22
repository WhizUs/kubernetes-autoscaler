package exoscale

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
	"k8s.io/klog/v2"
)

// Cache TTL durations for different resource types
const (
	// cacheTTLInstance defines how long compute instance data is cached
	cacheTTLInstance = 5 * time.Minute

	// cacheTTLInstancePool defines how long instance pool data is cached
	cacheTTLInstancePool = 5 * time.Minute

	// cacheTTLSKSCluster defines how long SKS cluster data is cached
	cacheTTLSKSCluster = 10 * time.Minute

	// cacheTTLQuota defines how long quota information is cached
	cacheTTLQuota = 1 * time.Hour

	// cacheJitterMinPercent defines the minimum jitter percentage added to TTL
	cacheJitterMinPercent = 10

	// cacheJitterMaxPercent defines the maximum jitter percentage added to TTL
	cacheJitterMaxPercent = 30
)

// Cache entry structs with TTL and expiration tracking

// cachedInstance represents a cached compute instance with expiration metadata
type cachedInstance struct {
	data      *egoscale.Instance
	fetchedAt time.Time
	ttl       time.Duration
}

// isExpired checks if the cached instance has exceeded its TTL
func (c *cachedInstance) isExpired() bool {
	return time.Since(c.fetchedAt) > c.ttl
}

// cachedInstancePool represents a cached instance pool with expiration metadata
type cachedInstancePool struct {
	data      *egoscale.InstancePool
	fetchedAt time.Time
	ttl       time.Duration
}

// isExpired checks if the cached instance pool has exceeded its TTL
func (c *cachedInstancePool) isExpired() bool {
	return time.Since(c.fetchedAt) > c.ttl
}

// cachedSKSCluster represents a cached SKS cluster with expiration metadata
type cachedSKSCluster struct {
	data      *egoscale.SKSCluster
	fetchedAt time.Time
	ttl       time.Duration
}

// isExpired checks if the cached SKS cluster has exceeded its TTL
func (c *cachedSKSCluster) isExpired() bool {
	return time.Since(c.fetchedAt) > c.ttl
}

// cachedQuota represents cached quota data with expiration metadata
type cachedQuota struct {
	data      *egoscale.Quota
	fetchedAt time.Time
	ttl       time.Duration
}

// isExpired checks if the cached quota has exceeded its TTL
func (c *cachedQuota) isExpired() bool {
	return time.Since(c.fetchedAt) > c.ttl
}

// cacheMetrics tracks cache hit and miss statistics per resource type
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

// exoscaleCache implements the exoscaleClient interface with caching
type exoscaleCache struct {
	// Wrapped client
	client exoscaleClient

	// Cache storage
	instancesCache     map[string]*cachedInstance
	instancePoolsCache map[string]*cachedInstancePool
	sksClustersCache   []*cachedSKSCluster
	quotaCache         map[string]*cachedQuota

	// Thread-safety
	mu sync.RWMutex

	// Configuration
	jitterEnabled bool

	// Metrics
	metrics cacheMetrics
}

// calculateTTLWithJitter adds random jitter (cacheJitterMinPercent-cacheJitterMaxPercent%) to the base TTL
func calculateTTLWithJitter(baseTTL time.Duration, jitterEnabled bool) time.Duration {
	if !jitterEnabled {
		return baseTTL
	}
	// Random jitter between cacheJitterMinPercent% and cacheJitterMaxPercent%
	jitterRange := cacheJitterMaxPercent - cacheJitterMinPercent + 1
	jitterPercent := cacheJitterMinPercent + rand.Intn(jitterRange)
	jitter := time.Duration(float64(baseTTL) * float64(jitterPercent) / 100.0)
	return baseTTL + jitter
}

// newExoscaleCache creates a new caching layer that wraps an exoscaleClient
func newExoscaleCache(client exoscaleClient, jitterEnabled bool) *exoscaleCache {
	return &exoscaleCache{
		client:             client,
		instancesCache:     make(map[string]*cachedInstance),
		instancePoolsCache: make(map[string]*cachedInstancePool),
		sksClustersCache:   make([]*cachedSKSCluster, 0),
		quotaCache:         make(map[string]*cachedQuota),
		jitterEnabled:      jitterEnabled,
	}
}

// GetInstance retrieves an instance with caching support
// Implements cach hit path, cache miss path, and stale data serving on API failure
func (c *exoscaleCache) GetInstance(ctx context.Context, zone string, id string) (*egoscale.Instance, error) {
	// Cache hit path - read lock for concurrent reads
	c.mu.RLock()
	cached, ok := c.instancesCache[id]
	c.mu.RUnlock()

	if ok && !cached.isExpired() {
		// Cache hit - return cached data
		atomic.AddInt64(&c.metrics.instanceHits, 1)
		klog.V(5).Infof("Cache hit for instance %s", id)
		return cached.data, nil
	}

	// Cache miss or expired - acquire write lock
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have updated)
	if cached, ok := c.instancesCache[id]; ok && !cached.isExpired() {
		atomic.AddInt64(&c.metrics.instanceHits, 1)
		return cached.data, nil
	}

	// Fetch from API
	instance, err := c.client.GetInstance(ctx, zone, id)
	atomic.AddInt64(&c.metrics.instanceMisses, 1)

	if err != nil {
		// API failure - serve stale data if available (graceful degradation)
		if cached != nil {
			klog.Warningf("API call failed for instance %s, serving stale data: %v", id, err)
			// Extend TTL to retry later
			cached.ttl = calculateTTLWithJitter(cacheTTLInstance, c.jitterEnabled)
			cached.fetchedAt = time.Now()
			return cached.data, nil
		}
		// No stale data available
		klog.V(3).Infof("Cache miss for instance %s, API call failed: %v", id, err)
		return nil, err
	}

	// Store in cache with TTL + jitter
	c.instancesCache[id] = &cachedInstance{
		data:      instance,
		fetchedAt: time.Now(),
		ttl:       calculateTTLWithJitter(cacheTTLInstance, c.jitterEnabled),
	}

	klog.V(3).Infof("Cache miss for instance %s, fetched from API and stored", id)
	return instance, nil
}

// GetInstancePool retrieves an instance pool with caching support
func (c *exoscaleCache) GetInstancePool(ctx context.Context, zone string, id string) (*egoscale.InstancePool, error) {
	// Cache hit path
	c.mu.RLock()
	cached, ok := c.instancePoolsCache[id]
	c.mu.RUnlock()

	if ok && !cached.isExpired() {
		atomic.AddInt64(&c.metrics.instancePoolHits, 1)
		klog.V(5).Infof("Cache hit for instance pool %s", id)
		return cached.data, nil
	}

	// Cache miss or expired
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if cached, ok := c.instancePoolsCache[id]; ok && !cached.isExpired() {
		atomic.AddInt64(&c.metrics.instancePoolHits, 1)
		return cached.data, nil
	}

	// Fetch from API
	pool, err := c.client.GetInstancePool(ctx, zone, id)
	atomic.AddInt64(&c.metrics.instancePoolMisses, 1)

	if err != nil {
		// Serve stale data on API failure
		if cached != nil {
			klog.Warningf("API call failed for instance pool %s, serving stale data: %v", id, err)
			cached.ttl = calculateTTLWithJitter(cacheTTLInstancePool, c.jitterEnabled)
			cached.fetchedAt = time.Now()
			return cached.data, nil
		}
		klog.V(3).Infof("Cache miss for instance pool %s, API call failed: %v", id, err)
		return nil, err
	}

	// Store in cache
	c.instancePoolsCache[id] = &cachedInstancePool{
		data:      pool,
		fetchedAt: time.Now(),
		ttl:       calculateTTLWithJitter(cacheTTLInstancePool, c.jitterEnabled),
	}

	klog.V(3).Infof("Cache miss for instance pool %s, fetched from API and stored", id)
	return pool, nil
}

// ListSKSClusters retrieves SKS clusters with list caching
func (c *exoscaleCache) ListSKSClusters(ctx context.Context, zone string) ([]*egoscale.SKSCluster, error) {
	// Check if we have cached SKS clusters that are still valid
	c.mu.RLock()
	if len(c.sksClustersCache) > 0 && !c.sksClustersCache[0].isExpired() {
		c.mu.RUnlock()
		atomic.AddInt64(&c.metrics.sksClusterHits, 1)
		klog.V(5).Infof("Cache hit for SKS clusters list")
		// Extract data from cached entries
		result := make([]*egoscale.SKSCluster, len(c.sksClustersCache))
		for i, cached := range c.sksClustersCache {
			result[i] = cached.data
		}
		return result, nil
	}
	c.mu.RUnlock()

	// Cache miss or expired
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if len(c.sksClustersCache) > 0 && !c.sksClustersCache[0].isExpired() {
		atomic.AddInt64(&c.metrics.sksClusterHits, 1)
		result := make([]*egoscale.SKSCluster, len(c.sksClustersCache))
		for i, cached := range c.sksClustersCache {
			result[i] = cached.data
		}
		return result, nil
	}

	// Fetch from API
	clusters, err := c.client.ListSKSClusters(ctx, zone)
	atomic.AddInt64(&c.metrics.sksClusterMisses, 1)

	if err != nil {
		// Serve stale data on API failure
		if len(c.sksClustersCache) > 0 {
			klog.Warningf("API call failed for SKS clusters, serving stale data: %v", err)
			// Extend TTL on all cached entries
			now := time.Now()
			ttl := calculateTTLWithJitter(cacheTTLSKSCluster, c.jitterEnabled)
			for _, cached := range c.sksClustersCache {
				cached.fetchedAt = now
				cached.ttl = ttl
			}
			result := make([]*egoscale.SKSCluster, len(c.sksClustersCache))
			for i, cached := range c.sksClustersCache {
				result[i] = cached.data
			}
			return result, nil
		}
		klog.V(3).Infof("Cache miss for SKS clusters, API call failed: %v", err)
		return nil, err
	}

	// Store in cache
	now := time.Now()
	ttl := calculateTTLWithJitter(cacheTTLSKSCluster, c.jitterEnabled)
	c.sksClustersCache = make([]*cachedSKSCluster, len(clusters))
	for i, cluster := range clusters {
		c.sksClustersCache[i] = &cachedSKSCluster{
			data:      cluster,
			fetchedAt: now,
			ttl:       ttl,
		}
	}

	klog.V(3).Infof("Cache miss for SKS clusters, fetched %d clusters from API", len(clusters))
	return clusters, nil
}

// GetQuota retrieves quota information with 1-hour TTL caching
func (c *exoscaleCache) GetQuota(ctx context.Context, zone string, resource string) (*egoscale.Quota, error) {
	// Cache hit path
	c.mu.RLock()
	cached, ok := c.quotaCache[resource]
	c.mu.RUnlock()

	if ok && !cached.isExpired() {
		atomic.AddInt64(&c.metrics.quotaHits, 1)
		klog.V(5).Infof("Cache hit for quota %s", resource)
		return cached.data, nil
	}

	// Cache miss or expired
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check
	if cached, ok := c.quotaCache[resource]; ok && !cached.isExpired() {
		atomic.AddInt64(&c.metrics.quotaHits, 1)
		return cached.data, nil
	}

	// Fetch from API
	quota, err := c.client.GetQuota(ctx, zone, resource)
	atomic.AddInt64(&c.metrics.quotaMisses, 1)

	if err != nil {
		// Serve stale data on API failure
		if cached != nil {
			klog.Warningf("API call failed for quota %s, serving stale data: %v", resource, err)
			cached.ttl = calculateTTLWithJitter(cacheTTLQuota, c.jitterEnabled)
			cached.fetchedAt = time.Now()
			return cached.data, nil
		}
		klog.V(3).Infof("Cache miss for quota %s, API call failed: %v", resource, err)
		return nil, err
	}

	// Store in cache with 1-hour TTL
	c.quotaCache[resource] = &cachedQuota{
		data:      quota,
		fetchedAt: time.Now(),
		ttl:       calculateTTLWithJitter(cacheTTLQuota, c.jitterEnabled),
	}

	klog.V(3).Infof("Cache miss for quota %s, fetched from API and stored", resource)
	return quota, nil
}

// Pass-through methods (no caching for write operations)

// EvictInstancePoolMembers passes through to the underlying client (write operation)
func (c *exoscaleCache) EvictInstancePoolMembers(ctx context.Context, zone string, instancePool *egoscale.InstancePool, members []string) error {
	return c.client.EvictInstancePoolMembers(ctx, zone, instancePool, members)
}

// EvictSKSNodepoolMembers passes through to the underlying client (write operation)
func (c *exoscaleCache) EvictSKSNodepoolMembers(ctx context.Context, zone string, cluster *egoscale.SKSCluster, nodepool *egoscale.SKSNodepool, members []string) error {
	return c.client.EvictSKSNodepoolMembers(ctx, zone, cluster, nodepool, members)
}

// ScaleInstancePool passes through to the underlying client (write operation)
func (c *exoscaleCache) ScaleInstancePool(ctx context.Context, zone string, instancePool *egoscale.InstancePool, size int64) error {
	return c.client.ScaleInstancePool(ctx, zone, instancePool, size)
}

// ScaleSKSNodepool passes through to the underlying client (write operation)
func (c *exoscaleCache) ScaleSKSNodepool(ctx context.Context, zone string, cluster *egoscale.SKSCluster, nodepool *egoscale.SKSNodepool, size int64) error {
	return c.client.ScaleSKSNodepool(ctx, zone, cluster, nodepool, size)
}

// Observability and management methods

// CacheMetrics contains cache performance statistics
type CacheMetrics struct {
	InstanceHits        int64
	InstanceMisses      int64
	InstanceHitRate     float64
	InstancePoolHits    int64
	InstancePoolMisses  int64
	InstancePoolHitRate float64
	SKSClusterHits      int64
	SKSClusterMisses    int64
	SKSClusterHitRate   float64
	QuotaHits           int64
	QuotaMisses         int64
	QuotaHitRate        float64
	TotalHits           int64
	TotalMisses         int64
	OverallHitRate      float64
}

// GetMetrics returns current cache performance metrics with hit rates
func (c *exoscaleCache) GetMetrics() CacheMetrics {
	// Read metrics atomically (no lock needed for atomic reads)
	instanceHits := atomic.LoadInt64(&c.metrics.instanceHits)
	instanceMisses := atomic.LoadInt64(&c.metrics.instanceMisses)
	instancePoolHits := atomic.LoadInt64(&c.metrics.instancePoolHits)
	instancePoolMisses := atomic.LoadInt64(&c.metrics.instancePoolMisses)
	sksClusterHits := atomic.LoadInt64(&c.metrics.sksClusterHits)
	sksClusterMisses := atomic.LoadInt64(&c.metrics.sksClusterMisses)
	quotaHits := atomic.LoadInt64(&c.metrics.quotaHits)
	quotaMisses := atomic.LoadInt64(&c.metrics.quotaMisses)

	// Calculate hit rates
	instanceHitRate := calculateHitRate(instanceHits, instanceMisses)
	instancePoolHitRate := calculateHitRate(instancePoolHits, instancePoolMisses)
	sksClusterHitRate := calculateHitRate(sksClusterHits, sksClusterMisses)
	quotaHitRate := calculateHitRate(quotaHits, quotaMisses)

	totalHits := instanceHits + instancePoolHits + sksClusterHits + quotaHits
	totalMisses := instanceMisses + instancePoolMisses + sksClusterMisses + quotaMisses
	overallHitRate := calculateHitRate(totalHits, totalMisses)

	return CacheMetrics{
		InstanceHits:        instanceHits,
		InstanceMisses:      instanceMisses,
		InstanceHitRate:     instanceHitRate,
		InstancePoolHits:    instancePoolHits,
		InstancePoolMisses:  instancePoolMisses,
		InstancePoolHitRate: instancePoolHitRate,
		SKSClusterHits:      sksClusterHits,
		SKSClusterMisses:    sksClusterMisses,
		SKSClusterHitRate:   sksClusterHitRate,
		QuotaHits:           quotaHits,
		QuotaMisses:         quotaMisses,
		QuotaHitRate:        quotaHitRate,
		TotalHits:           totalHits,
		TotalMisses:         totalMisses,
		OverallHitRate:      overallHitRate,
	}
}

// calculateHitRate computes the hit rate as hits / (hits + misses)
// Returns 0.0 if there are no operations yet
func calculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// InvalidateCache clears all cached entries and resets metrics
// Useful for testing, debugging, or forcing a full refresh
func (c *exoscaleCache) InvalidateCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear all cache maps
	c.instancesCache = make(map[string]*cachedInstance)
	c.instancePoolsCache = make(map[string]*cachedInstancePool)
	c.sksClustersCache = make([]*cachedSKSCluster, 0)
	c.quotaCache = make(map[string]*cachedQuota)

	// Reset metrics
	atomic.StoreInt64(&c.metrics.instanceHits, 0)
	atomic.StoreInt64(&c.metrics.instanceMisses, 0)
	atomic.StoreInt64(&c.metrics.instancePoolHits, 0)
	atomic.StoreInt64(&c.metrics.instancePoolMisses, 0)
	atomic.StoreInt64(&c.metrics.sksClusterHits, 0)
	atomic.StoreInt64(&c.metrics.sksClusterMisses, 0)
	atomic.StoreInt64(&c.metrics.quotaHits, 0)
	atomic.StoreInt64(&c.metrics.quotaMisses, 0)

	klog.V(3).Infof("Cache invalidated: all entries and metrics cleared")
}

// SetJitterEnabled enables or disables TTL jitter
// Jitter should be enabled in production to prevent thundering herd
func (c *exoscaleCache) SetJitterEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.jitterEnabled = enabled
	klog.V(3).Infof("Cache jitter enabled set to: %v", enabled)
}
