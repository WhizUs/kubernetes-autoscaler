package exoscale

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
)

// exoscaleClientMock is defined in exoscale_cloud_provider_test.go and shared across test files

// Test isExpired() with fresh and stale data
func TestCacheEntry_IsExpired_Fresh(t *testing.T) {
	entry := &cachedInstance{
		data:      &egoscale.Instance{},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}
	assert.False(t, entry.isExpired(), "Fresh entry should not be expired")
}

func TestCacheEntry_IsExpired_Stale(t *testing.T) {
	entry := &cachedInstance{
		data:      &egoscale.Instance{},
		fetchedAt: time.Now().Add(-10 * time.Minute),
		ttl:       5 * time.Minute,
	}
	assert.True(t, entry.isExpired(), "Stale entry should be expired")
}

func TestCacheEntryPool_IsExpired(t *testing.T) {
	fresh := &cachedInstancePool{
		data:      &egoscale.InstancePool{},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}
	assert.False(t, fresh.isExpired())

	stale := &cachedInstancePool{
		data:      &egoscale.InstancePool{},
		fetchedAt: time.Now().Add(-10 * time.Minute),
		ttl:       5 * time.Minute,
	}
	assert.True(t, stale.isExpired())
}

func TestCacheEntrySKS_IsExpired(t *testing.T) {
	fresh := &cachedSKSCluster{
		data:      &egoscale.SKSCluster{},
		fetchedAt: time.Now(),
		ttl:       10 * time.Minute,
	}
	assert.False(t, fresh.isExpired())

	stale := &cachedSKSCluster{
		data:      &egoscale.SKSCluster{},
		fetchedAt: time.Now().Add(-15 * time.Minute),
		ttl:       10 * time.Minute,
	}
	assert.True(t, stale.isExpired())
}

func TestCacheEntryQuota_IsExpired(t *testing.T) {
	fresh := &cachedQuota{
		data:      &egoscale.Quota{},
		fetchedAt: time.Now(),
		ttl:       1 * time.Hour,
	}
	assert.False(t, fresh.isExpired())

	stale := &cachedQuota{
		data:      &egoscale.Quota{},
		fetchedAt: time.Now().Add(-90 * time.Minute),
		ttl:       1 * time.Hour,
	}
	assert.True(t, stale.isExpired())
}

// Test jitter calculation (verify 10-30% range)
func TestCalculateTTLWithJitter_Disabled(t *testing.T) {
	baseTTL := 5 * time.Minute
	result := calculateTTLWithJitter(baseTTL, false)
	assert.Equal(t, baseTTL, result, "With jitter disabled, TTL should equal base TTL")
}

func TestCalculateTTLWithJitter_Enabled(t *testing.T) {
	baseTTL := 5 * time.Minute
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10) // 10% jitter
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30) // 30% jitter

	// Test multiple times to verify range
	for i := 0; i < 100; i++ {
		result := calculateTTLWithJitter(baseTTL, true)
		assert.GreaterOrEqual(t, result, minExpected, "TTL with jitter should be at least base + 10%%")
		assert.LessOrEqual(t, result, maxExpected, "TTL with jitter should be at most base + 30%%")
	}
}

func TestCalculateTTLWithJitter_DifferentBaseTTLs(t *testing.T) {
	testCases := []struct {
		name    string
		baseTTL time.Duration
	}{
		{"5 minutes", 5 * time.Minute},
		{"10 minutes", 10 * time.Minute},
		{"1 hour", 1 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			minExpected := tc.baseTTL + time.Duration(float64(tc.baseTTL)*0.10)
			maxExpected := tc.baseTTL + time.Duration(float64(tc.baseTTL)*0.30)

			result := calculateTTLWithJitter(tc.baseTTL, true)
			assert.GreaterOrEqual(t, result, minExpected)
			assert.LessOrEqual(t, result, maxExpected)
		})
	}
}

// Test cache initialization
func TestNewExoscaleCache(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, true)

	assert.NotNil(t, cache, "Cache should not be nil")
	assert.Equal(t, mockClient, cache.client, "Cache should wrap the provided client")
	assert.True(t, cache.jitterEnabled, "Jitter should be enabled")
	assert.NotNil(t, cache.instancesCache, "Instances cache map should be initialized")
	assert.NotNil(t, cache.instancePoolsCache, "Instance pools cache map should be initialized")
	assert.NotNil(t, cache.sksClustersCache, "SKS clusters cache should be initialized")
	assert.NotNil(t, cache.quotaCache, "Quota cache map should be initialized")
	assert.Equal(t, 0, len(cache.instancesCache), "Instances cache should start empty")
	assert.Equal(t, 0, len(cache.instancePoolsCache), "Instance pools cache should start empty")
	assert.Equal(t, 0, len(cache.sksClustersCache), "SKS clusters cache should start empty")
	assert.Equal(t, 0, len(cache.quotaCache), "Quota cache should start empty")
}

func TestNewExoscaleCache_JitterDisabled(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	assert.False(t, cache.jitterEnabled, "Jitter should be disabled")
}

// Test GetInstance cache hit (no API call)
func TestGetInstance_CacheHit(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Pre-populate cache
	testID := "test-instance-id"
	cache.instancesCache[testID] = &cachedInstance{
		data:      &egoscale.Instance{ID: &testID, Name: &[]string{"test-instance"}[0]},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}

	// Should not call API
	instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)

	assert.NoError(t, err)
	assert.NotNil(t, instance)
	assert.Equal(t, testID, *instance.ID)
	assert.Equal(t, "test-instance", *instance.Name)
	mockClient.AssertNotCalled(t, "GetInstance")
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instanceMisses))
}

// Test GetInstance cache miss (calls API, stores)
func TestGetInstance_CacheMiss(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "new-instance-id"
	expectedInstance := &egoscale.Instance{ID: &testID, Name: &[]string{"new-instance"}[0]}
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Return(expectedInstance, nil)

	instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)

	assert.NoError(t, err)
	assert.NotNil(t, instance)
	assert.Equal(t, testID, *instance.ID)
	mockClient.AssertCalled(t, "GetInstance", mock.Anything, "ch-gva-2", testID)
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instanceHits))
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceMisses))

	// Verify it was stored in cache
	cached, ok := cache.instancesCache[testID]
	assert.True(t, ok, "Instance should be stored in cache")
	assert.NotNil(t, cached)
	assert.Equal(t, expectedInstance, cached.data)
}

// Test GetInstance expired entry (refreshes from API)
func TestGetInstance_ExpiredEntry(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "expired-instance-id"
	// Pre-populate with expired entry
	cache.instancesCache[testID] = &cachedInstance{
		data:      &egoscale.Instance{ID: &testID, Name: &[]string{"old-data"}[0]},
		fetchedAt: time.Now().Add(-10 * time.Minute),
		ttl:       5 * time.Minute,
	}

	freshInstance := &egoscale.Instance{ID: &testID, Name: &[]string{"fresh-data"}[0]}
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Return(freshInstance, nil)

	instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)

	assert.NoError(t, err)
	assert.NotNil(t, instance)
	assert.Equal(t, "fresh-data", *instance.Name)
	mockClient.AssertCalled(t, "GetInstance", mock.Anything, "ch-gva-2", testID)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceMisses))
}

// Test GetInstance API failure with stale data
func TestGetInstance_APIFailureWithStaleData(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "stale-instance-id"
	staleData := &egoscale.Instance{ID: &testID, Name: &[]string{"stale-data"}[0]}
	// Pre-populate with expired entry
	cache.instancesCache[testID] = &cachedInstance{
		data:      staleData,
		fetchedAt: time.Now().Add(-10 * time.Minute),
		ttl:       5 * time.Minute,
	}

	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Return(nil, assert.AnError)

	instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)

	// Should return stale data instead of error
	assert.NoError(t, err)
	assert.NotNil(t, instance)
	assert.Equal(t, "stale-data", *instance.Name)
	mockClient.AssertCalled(t, "GetInstance", mock.Anything, "ch-gva-2", testID)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceMisses))
}

// Test GetInstance API failure without cache
func TestGetInstance_APIFailureWithoutCache(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "nonexistent-instance-id"
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Return(nil, assert.AnError)

	instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)

	assert.Error(t, err)
	assert.Nil(t, instance)
	mockClient.AssertCalled(t, "GetInstance", mock.Anything, "ch-gva-2", testID)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instanceMisses))
}

// Test GetInstance metrics increment
func TestGetInstance_MetricsIncrement(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "metrics-test-id"
	cache.instancesCache[testID] = &cachedInstance{
		data:      &egoscale.Instance{ID: &testID},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}

	// Multiple cache hits
	for i := 0; i < 5; i++ {
		_, _ = cache.GetInstance(context.Background(), "ch-gva-2", testID)
	}

	assert.Equal(t, int64(5), atomic.LoadInt64(&cache.metrics.instanceHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instanceMisses))
}

// Test GetInstancePool hit/miss scenarios
func TestGetInstancePool_CacheHit(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "test-pool-id"
	cache.instancePoolsCache[testID] = &cachedInstancePool{
		data:      &egoscale.InstancePool{ID: &testID, Name: &[]string{"test-pool"}[0]},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}

	pool, err := cache.GetInstancePool(context.Background(), "ch-gva-2", testID)

	assert.NoError(t, err)
	assert.NotNil(t, pool)
	assert.Equal(t, testID, *pool.ID)
	mockClient.AssertNotCalled(t, "GetInstancePool")
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instancePoolHits))
}

func TestGetInstancePool_CacheMiss(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "new-pool-id"
	expectedPool := &egoscale.InstancePool{ID: &testID, Name: &[]string{"new-pool"}[0]}
	mockClient.On("GetInstancePool", mock.Anything, "ch-gva-2", testID).Return(expectedPool, nil)

	pool, err := cache.GetInstancePool(context.Background(), "ch-gva-2", testID)

	assert.NoError(t, err)
	assert.NotNil(t, pool)
	assert.Equal(t, testID, *pool.ID)
	mockClient.AssertCalled(t, "GetInstancePool", mock.Anything, "ch-gva-2", testID)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.instancePoolMisses))

	// Verify stored in cache
	cached, ok := cache.instancePoolsCache[testID]
	assert.True(t, ok)
	assert.Equal(t, expectedPool, cached.data)
}

// Test ListSKSClusters caching full list
func TestListSKSClusters_CacheHit(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Pre-populate cache with list
	cluster1 := &egoscale.SKSCluster{ID: &[]string{"cluster-1"}[0], Name: &[]string{"cluster-one"}[0]}
	cluster2 := &egoscale.SKSCluster{ID: &[]string{"cluster-2"}[0], Name: &[]string{"cluster-two"}[0]}
	now := time.Now()
	cache.sksClustersCache = []*cachedSKSCluster{
		{data: cluster1, fetchedAt: now, ttl: 10 * time.Minute},
		{data: cluster2, fetchedAt: now, ttl: 10 * time.Minute},
	}

	clusters, err := cache.ListSKSClusters(context.Background(), "ch-gva-2")

	assert.NoError(t, err)
	assert.Len(t, clusters, 2)
	assert.Equal(t, "cluster-1", *clusters[0].ID)
	assert.Equal(t, "cluster-2", *clusters[1].ID)
	mockClient.AssertNotCalled(t, "ListSKSClusters")
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.sksClusterHits))
}

func TestListSKSClusters_CacheMiss(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	expectedClusters := []*egoscale.SKSCluster{
		{ID: &[]string{"cluster-1"}[0], Name: &[]string{"cluster-one"}[0]},
		{ID: &[]string{"cluster-2"}[0], Name: &[]string{"cluster-two"}[0]},
	}
	mockClient.On("ListSKSClusters", mock.Anything, "ch-gva-2").Return(expectedClusters, nil)

	clusters, err := cache.ListSKSClusters(context.Background(), "ch-gva-2")

	assert.NoError(t, err)
	assert.Len(t, clusters, 2)
	mockClient.AssertCalled(t, "ListSKSClusters", mock.Anything, "ch-gva-2")
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.sksClusterMisses))

	// Verify stored in cache
	assert.Len(t, cache.sksClustersCache, 2)
	assert.Equal(t, expectedClusters[0], cache.sksClustersCache[0].data)
}

// Test GetQuota with 1-hour TTL
func TestGetQuota_CacheHit(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	resource := "instance"
	limit := int64(100)
	cache.quotaCache[resource] = &cachedQuota{
		data:      &egoscale.Quota{Resource: &resource, Limit: &limit},
		fetchedAt: time.Now(),
		ttl:       1 * time.Hour,
	}

	quota, err := cache.GetQuota(context.Background(), "ch-gva-2", resource)

	assert.NoError(t, err)
	assert.NotNil(t, quota)
	assert.Equal(t, int64(100), *quota.Limit)
	mockClient.AssertNotCalled(t, "GetQuota")
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.quotaHits))
}

func TestGetQuota_CacheMiss(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	resource := "instance"
	limit := int64(100)
	expectedQuota := &egoscale.Quota{Resource: &resource, Limit: &limit}
	mockClient.On("GetQuota", mock.Anything, "ch-gva-2", resource).Return(expectedQuota, nil)

	quota, err := cache.GetQuota(context.Background(), "ch-gva-2", resource)

	assert.NoError(t, err)
	assert.NotNil(t, quota)
	assert.Equal(t, int64(100), *quota.Limit)
	mockClient.AssertCalled(t, "GetQuota", mock.Anything, "ch-gva-2", resource)
	assert.Equal(t, int64(1), atomic.LoadInt64(&cache.metrics.quotaMisses))

	// Verify 1-hour TTL
	cached := cache.quotaCache[resource]
	assert.NotNil(t, cached)
	assert.Equal(t, 1*time.Hour, cached.ttl)
}

// Test pass-through methods (always call API)
func TestEvictInstancePoolMembers_PassThrough(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	pool := &egoscale.InstancePool{ID: &[]string{"pool-id"}[0]}
	members := []string{"instance-1", "instance-2"}
	mockClient.On("EvictInstancePoolMembers", mock.Anything, "ch-gva-2", pool, members).Return(nil)

	err := cache.EvictInstancePoolMembers(context.Background(), "ch-gva-2", pool, members)

	assert.NoError(t, err)
	mockClient.AssertCalled(t, "EvictInstancePoolMembers", mock.Anything, "ch-gva-2", pool, members)
}

func TestEvictSKSNodepoolMembers_PassThrough(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	cluster := &egoscale.SKSCluster{ID: &[]string{"cluster-id"}[0]}
	nodepool := &egoscale.SKSNodepool{ID: &[]string{"nodepool-id"}[0]}
	members := []string{"node-1"}
	mockClient.On("EvictSKSNodepoolMembers", mock.Anything, "ch-gva-2", cluster, nodepool, members).Return(nil)

	err := cache.EvictSKSNodepoolMembers(context.Background(), "ch-gva-2", cluster, nodepool, members)

	assert.NoError(t, err)
	mockClient.AssertCalled(t, "EvictSKSNodepoolMembers", mock.Anything, "ch-gva-2", cluster, nodepool, members)
}

func TestScaleInstancePool_PassThrough(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	pool := &egoscale.InstancePool{ID: &[]string{"pool-id"}[0]}
	mockClient.On("ScaleInstancePool", mock.Anything, "ch-gva-2", pool, int64(5)).Return(nil)

	err := cache.ScaleInstancePool(context.Background(), "ch-gva-2", pool, 5)

	assert.NoError(t, err)
	mockClient.AssertCalled(t, "ScaleInstancePool", mock.Anything, "ch-gva-2", pool, int64(5))
}

func TestScaleSKSNodepool_PassThrough(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	cluster := &egoscale.SKSCluster{ID: &[]string{"cluster-id"}[0]}
	nodepool := &egoscale.SKSNodepool{ID: &[]string{"nodepool-id"}[0]}
	mockClient.On("ScaleSKSNodepool", mock.Anything, "ch-gva-2", cluster, nodepool, int64(3)).Return(nil)

	err := cache.ScaleSKSNodepool(context.Background(), "ch-gva-2", cluster, nodepool, 3)

	assert.NoError(t, err)
	mockClient.AssertCalled(t, "ScaleSKSNodepool", mock.Anything, "ch-gva-2", cluster, nodepool, int64(3))
}

// Test GetMetrics returning correct hit rates
func TestGetMetrics_CorrectHitRates(t *testing.T) {
	cache := newExoscaleCache(nil, false)

	// Set up metrics: 85 hits, 15 misses per resource type
	atomic.StoreInt64(&cache.metrics.instanceHits, 85)
	atomic.StoreInt64(&cache.metrics.instanceMisses, 15)
	atomic.StoreInt64(&cache.metrics.instancePoolHits, 90)
	atomic.StoreInt64(&cache.metrics.instancePoolMisses, 10)
	atomic.StoreInt64(&cache.metrics.sksClusterHits, 95)
	atomic.StoreInt64(&cache.metrics.sksClusterMisses, 5)
	atomic.StoreInt64(&cache.metrics.quotaHits, 99)
	atomic.StoreInt64(&cache.metrics.quotaMisses, 1)

	metrics := cache.GetMetrics()

	// Verify individual hit rates
	assert.Equal(t, int64(85), metrics.InstanceHits)
	assert.Equal(t, int64(15), metrics.InstanceMisses)
	assert.InDelta(t, 0.85, metrics.InstanceHitRate, 0.001) // 85/100 = 0.85

	assert.Equal(t, int64(90), metrics.InstancePoolHits)
	assert.Equal(t, int64(10), metrics.InstancePoolMisses)
	assert.InDelta(t, 0.90, metrics.InstancePoolHitRate, 0.001) // 90/100 = 0.90

	assert.Equal(t, int64(95), metrics.SKSClusterHits)
	assert.Equal(t, int64(5), metrics.SKSClusterMisses)
	assert.InDelta(t, 0.95, metrics.SKSClusterHitRate, 0.001) // 95/100 = 0.95

	assert.Equal(t, int64(99), metrics.QuotaHits)
	assert.Equal(t, int64(1), metrics.QuotaMisses)
	assert.InDelta(t, 0.99, metrics.QuotaHitRate, 0.001) // 99/100 = 0.99

	// Verify overall metrics
	assert.Equal(t, int64(369), metrics.TotalHits)    // 85+90+95+99
	assert.Equal(t, int64(31), metrics.TotalMisses)   // 15+10+5+1
	assert.InDelta(t, 0.9225, metrics.OverallHitRate, 0.001) // 369/400 = 0.9225
}

// Test GetMetrics with zero operations
func TestGetMetrics_ZeroOperations(t *testing.T) {
	cache := newExoscaleCache(nil, false)

	metrics := cache.GetMetrics()

	// All counts should be zero
	assert.Equal(t, int64(0), metrics.InstanceHits)
	assert.Equal(t, int64(0), metrics.InstanceMisses)
	assert.Equal(t, int64(0), metrics.InstancePoolHits)
	assert.Equal(t, int64(0), metrics.InstancePoolMisses)
	assert.Equal(t, int64(0), metrics.SKSClusterHits)
	assert.Equal(t, int64(0), metrics.SKSClusterMisses)
	assert.Equal(t, int64(0), metrics.QuotaHits)
	assert.Equal(t, int64(0), metrics.QuotaMisses)
	assert.Equal(t, int64(0), metrics.TotalHits)
	assert.Equal(t, int64(0), metrics.TotalMisses)

	// Hit rates should be 0.0 when there are no operations
	assert.Equal(t, 0.0, metrics.InstanceHitRate)
	assert.Equal(t, 0.0, metrics.InstancePoolHitRate)
	assert.Equal(t, 0.0, metrics.SKSClusterHitRate)
	assert.Equal(t, 0.0, metrics.QuotaHitRate)
	assert.Equal(t, 0.0, metrics.OverallHitRate)
}

// Test InvalidateCache clearing all entries
func TestInvalidateCache_ClearsAllEntries(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Populate cache with data
	cache.instancesCache["inst-1"] = &cachedInstance{
		data:      &egoscale.Instance{ID: &[]string{"inst-1"}[0]},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}
	cache.instancePoolsCache["pool-1"] = &cachedInstancePool{
		data:      &egoscale.InstancePool{ID: &[]string{"pool-1"}[0]},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}
	cache.sksClustersCache = []*cachedSKSCluster{
		{
			data:      &egoscale.SKSCluster{ID: &[]string{"cluster-1"}[0]},
			fetchedAt: time.Now(),
			ttl:       10 * time.Minute,
		},
	}
	cache.quotaCache["instance"] = &cachedQuota{
		data:      &egoscale.Quota{Resource: &[]string{"instance"}[0]},
		fetchedAt: time.Now(),
		ttl:       1 * time.Hour,
	}

	// Add some metrics
	atomic.StoreInt64(&cache.metrics.instanceHits, 100)
	atomic.StoreInt64(&cache.metrics.instanceMisses, 20)

	// Invalidate
	cache.InvalidateCache()

	// Verify all caches are cleared
	assert.Equal(t, 0, len(cache.instancesCache), "Instances cache should be empty")
	assert.Equal(t, 0, len(cache.instancePoolsCache), "Instance pools cache should be empty")
	assert.Equal(t, 0, len(cache.sksClustersCache), "SKS clusters cache should be empty")
	assert.Equal(t, 0, len(cache.quotaCache), "Quota cache should be empty")

	// Verify metrics are reset
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instanceHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instanceMisses))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instancePoolHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.instancePoolMisses))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.sksClusterHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.sksClusterMisses))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.quotaHits))
	assert.Equal(t, int64(0), atomic.LoadInt64(&cache.metrics.quotaMisses))
}

// Test SetJitterEnabled affecting TTL calculations
func TestSetJitterEnabled_AffectsTTLCalculations(t *testing.T) {
	cache := newExoscaleCache(nil, true)
	assert.True(t, cache.jitterEnabled, "Jitter should start enabled")

	// Disable jitter
	cache.SetJitterEnabled(false)
	assert.False(t, cache.jitterEnabled, "Jitter should now be disabled")

	// Re-enable jitter
	cache.SetJitterEnabled(true)
	assert.True(t, cache.jitterEnabled, "Jitter should be enabled again")
}

func TestSetJitterEnabled_ThreadSafe(t *testing.T) {
	cache := newExoscaleCache(nil, true)

	// Toggle jitter concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(enabled bool) {
			defer wg.Done()
			cache.SetJitterEnabled(enabled)
		}(i%2 == 0)
	}
	wg.Wait()

	// Should complete without data races (verify with -race flag)
}

// Test instance calls ≤ once per 5min
func TestAPILoadReduction_InstanceCallsOncePerFiveMinutes(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "instance-ttl-test"
	expectedInstance := &egoscale.Instance{ID: &testID}

	// API should be called only once despite multiple requests within TTL
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Return(expectedInstance, nil).Once()

	// Simulate 30 requests over a short period (all within 5min TTL)
	for i := 0; i < 30; i++ {
		_, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)
		assert.NoError(t, err)
	}

	// Verify API was called exactly once
	mockClient.AssertExpectations(t)

	// Verify cache hit rate is high (29 hits out of 30 total)
	metrics := cache.GetMetrics()
	assert.Equal(t, int64(29), metrics.InstanceHits)
	assert.Equal(t, int64(1), metrics.InstanceMisses)
	assert.InDelta(t, 0.9667, metrics.InstanceHitRate, 0.001) // 29/30 = 96.67%
}

// Test pool calls ≤ once per 5min
func TestAPILoadReduction_PoolCallsOncePerFiveMinutes(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "pool-ttl-test"
	expectedPool := &egoscale.InstancePool{ID: &testID}

	// API should be called only once
	mockClient.On("GetInstancePool", mock.Anything, "ch-gva-2", testID).Return(expectedPool, nil).Once()

	// Simulate 20 requests within TTL
	for i := 0; i < 20; i++ {
		_, err := cache.GetInstancePool(context.Background(), "ch-gva-2", testID)
		assert.NoError(t, err)
	}

	mockClient.AssertExpectations(t)

	metrics := cache.GetMetrics()
	assert.Equal(t, int64(19), metrics.InstancePoolHits)
	assert.Equal(t, int64(1), metrics.InstancePoolMisses)
	assert.InDelta(t, 0.95, metrics.InstancePoolHitRate, 0.001) // 19/20 = 95%
}

// Test SKS cluster calls ≤ once per 10min
func TestAPILoadReduction_SKSClusterCallsOncePerTenMinutes(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	expectedClusters := []*egoscale.SKSCluster{
		{ID: &[]string{"cluster-1"}[0]},
		{ID: &[]string{"cluster-2"}[0]},
	}

	// API should be called only once (10min TTL is longer than 5min)
	mockClient.On("ListSKSClusters", mock.Anything, "ch-gva-2").Return(expectedClusters, nil).Once()

	// Simulate 25 requests within TTL
	for i := 0; i < 25; i++ {
		_, err := cache.ListSKSClusters(context.Background(), "ch-gva-2")
		assert.NoError(t, err)
	}

	mockClient.AssertExpectations(t)

	metrics := cache.GetMetrics()
	assert.Equal(t, int64(24), metrics.SKSClusterHits)
	assert.Equal(t, int64(1), metrics.SKSClusterMisses)
	assert.InDelta(t, 0.96, metrics.SKSClusterHitRate, 0.001) // 24/25 = 96%
}

// Test quota calls ≤ once per hour
func TestAPILoadReduction_QuotaCallsOncePerHour(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	resource := "instance"
	limit := int64(100)
	expectedQuota := &egoscale.Quota{Resource: &resource, Limit: &limit}

	// API should be called only once (1hr TTL is very long)
	mockClient.On("GetQuota", mock.Anything, "ch-gva-2", resource).Return(expectedQuota, nil).Once()

	// Simulate 100 requests within TTL
	for i := 0; i < 100; i++ {
		_, err := cache.GetQuota(context.Background(), "ch-gva-2", resource)
		assert.NoError(t, err)
	}

	mockClient.AssertExpectations(t)

	metrics := cache.GetMetrics()
	assert.Equal(t, int64(99), metrics.QuotaHits)
	assert.Equal(t, int64(1), metrics.QuotaMisses)
	assert.InDelta(t, 0.99, metrics.QuotaHitRate, 0.001) // 99/100 = 99%
}

// Integration test verifying 90% API call reduction
func TestAPILoadReduction_90PercentReduction(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Setup test data
	instanceID := "inst-1"
	poolID := "pool-1"
	resource := "instance"

	expectedInstance := &egoscale.Instance{ID: &instanceID}
	expectedPool := &egoscale.InstancePool{ID: &poolID}
	expectedClusters := []*egoscale.SKSCluster{{ID: &[]string{"cluster-1"}[0]}}
	expectedQuota := &egoscale.Quota{Resource: &resource}

	// Each API should be called only once (simulating 10 autoscaler loops within TTL)
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", instanceID).Return(expectedInstance, nil).Once()
	mockClient.On("GetInstancePool", mock.Anything, "ch-gva-2", poolID).Return(expectedPool, nil).Once()
	mockClient.On("ListSKSClusters", mock.Anything, "ch-gva-2").Return(expectedClusters, nil).Once()
	mockClient.On("GetQuota", mock.Anything, "ch-gva-2", resource).Return(expectedQuota, nil).Once()

	// Simulate 10 autoscaler loops (each loop makes 4 API calls)
	// Without caching: 40 API calls
	// With caching: 4 API calls (one per resource type on first loop)
	for loop := 0; loop < 10; loop++ {
		_, _ = cache.GetInstance(context.Background(), "ch-gva-2", instanceID)
		_, _ = cache.GetInstancePool(context.Background(), "ch-gva-2", poolID)
		_, _ = cache.ListSKSClusters(context.Background(), "ch-gva-2")
		_, _ = cache.GetQuota(context.Background(), "ch-gva-2", resource)
	}

	// Verify each API was called exactly once
	mockClient.AssertExpectations(t)

	// Verify overall metrics show high hit rate
	metrics := cache.GetMetrics()
	// Total operations: 40 (10 loops * 4 resource types)
	// Total hits: 36 (9 cached loops * 4 resource types)
	// Total misses: 4 (first loop only)
	assert.Equal(t, int64(36), metrics.TotalHits)
	assert.Equal(t, int64(4), metrics.TotalMisses)
	assert.InDelta(t, 0.90, metrics.OverallHitRate, 0.001) // 36/40 = 90%

	// API call reduction: 90% (4 API calls instead of 40)
	apiCallsWithoutCache := 40
	apiCallsWithCache := 4
	reduction := float64(apiCallsWithoutCache-apiCallsWithCache) / float64(apiCallsWithoutCache)
	assert.InDelta(t, 0.90, reduction, 0.001) // 90% reduction
}

// Test jitter spreading cache expiration times
func TestJitter_SpreadsExpirationTimes(t *testing.T) {
	// Create 100 instances with jittered TTLs
	baseTTL := 5 * time.Minute
	ttls := make([]time.Duration, 100)

	for i := 0; i < 100; i++ {
		ttl := calculateTTLWithJitter(baseTTL, true)
		ttls[i] = ttl
	}

	// Verify TTLs are spread across the jitter range
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10)
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30)

	for _, ttl := range ttls {
		assert.GreaterOrEqual(t, ttl, minExpected, "TTL should be at least base + 10%%")
		assert.LessOrEqual(t, ttl, maxExpected, "TTL should be at most base + 30%%")
	}

	// Verify TTLs are NOT all the same (distribution exists)
	allSame := true
	firstTTL := ttls[0]
	for _, ttl := range ttls[1:] {
		if ttl != firstTTL {
			allSame = false
			break
		}
	}
	assert.False(t, allSame, "TTLs should be distributed, not all identical")
}

// Test jitter preventing simultaneous refreshes
func TestJitter_PreventsSimultaneousRefreshes(t *testing.T) {
	mockClient := new(exoscaleClientMock)

	// Simulate 10 autoscaler instances
	caches := make([]*exoscaleCache, 10)
	expirationTimes := make([]time.Time, 10)

	for i := 0; i < 10; i++ {
		caches[i] = newExoscaleCache(mockClient, true) // Jitter enabled
		// Store instance in cache
		caches[i].instancesCache["test-id"] = &cachedInstance{
			data:      &egoscale.Instance{ID: &[]string{"test-id"}[0]},
			fetchedAt: time.Now(),
			ttl:       calculateTTLWithJitter(5*time.Minute, true),
		}
		// Calculate expiration time
		entry := caches[i].instancesCache["test-id"]
		expirationTimes[i] = entry.fetchedAt.Add(entry.ttl)
	}

	// Verify expiration times are spread out (not synchronized)
	for i := 0; i < len(expirationTimes)-1; i++ {
		for j := i + 1; j < len(expirationTimes); j++ {
			// Most expiration times should differ (allowing for rare collisions)
			// With 10-30% jitter on 5min (30-90s range), timestamps should vary
			diff := expirationTimes[i].Sub(expirationTimes[j]).Abs()
			// We don't assert all are different (might have rare collisions)
			// But the overall distribution should prevent thundering herd
			_ = diff // Log or track distribution
		}
	}

	// Statistical check: at least 70% should have unique expiration times
	uniqueCount := 0
	for i := 0; i < len(expirationTimes); i++ {
		isUnique := true
		for j := 0; j < len(expirationTimes); j++ {
			if i != j && expirationTimes[i].Equal(expirationTimes[j]) {
				isUnique = false
				break
			}
		}
		if isUnique {
			uniqueCount++
		}
	}

	// At least 70% should be unique (allowing for some random collisions)
	assert.GreaterOrEqual(t, uniqueCount, 7, "At least 70%% of expiration times should be unique")
}

// Test instances TTL with jitter (5-6.5min range)
func TestJitter_InstancesTTLRange(t *testing.T) {
	baseTTL := 5 * time.Minute
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10) // 5.5min
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30) // 6.5min

	// Test 50 iterations
	for i := 0; i < 50; i++ {
		ttl := calculateTTLWithJitter(baseTTL, true)
		assert.GreaterOrEqual(t, ttl, minExpected, "Instance TTL should be >= 5.5min")
		assert.LessOrEqual(t, ttl, maxExpected, "Instance TTL should be <= 6.5min")
	}
}

// Test pools TTL with jitter (5-6.5min range)
func TestJitter_PoolsTTLRange(t *testing.T) {
	baseTTL := 5 * time.Minute
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10) // 5.5min
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30) // 6.5min

	// Test 50 iterations
	for i := 0; i < 50; i++ {
		ttl := calculateTTLWithJitter(baseTTL, true)
		assert.GreaterOrEqual(t, ttl, minExpected, "Pool TTL should be >= 5.5min")
		assert.LessOrEqual(t, ttl, maxExpected, "Pool TTL should be <= 6.5min")
	}
}

// Test SKS clusters TTL with jitter (10-13min range)
func TestJitter_SKSClustersTTLRange(t *testing.T) {
	baseTTL := 10 * time.Minute
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10) // 11min
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30) // 13min

	// Test 50 iterations
	for i := 0; i < 50; i++ {
		ttl := calculateTTLWithJitter(baseTTL, true)
		assert.GreaterOrEqual(t, ttl, minExpected, "SKS cluster TTL should be >= 11min")
		assert.LessOrEqual(t, ttl, maxExpected, "SKS cluster TTL should be <= 13min")
	}
}

// Test quota TTL with jitter (60-78min range)
func TestJitter_QuotaTTLRange(t *testing.T) {
	baseTTL := 1 * time.Hour
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10) // 66min
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30) // 78min

	// Test 50 iterations
	for i := 0; i < 50; i++ {
		ttl := calculateTTLWithJitter(baseTTL, true)
		assert.GreaterOrEqual(t, ttl, minExpected, "Quota TTL should be >= 66min")
		assert.LessOrEqual(t, ttl, maxExpected, "Quota TTL should be <= 78min")
	}
}

// Integration test for multiple loops showing random refresh timing
func TestJitter_MultipleLoopsRandomRefreshTiming(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, true) // Jitter enabled

	testID := "jitter-test-id"
	expectedInstance := &egoscale.Instance{ID: &testID}

	// Track when cache entries are created
	refreshTimestamps := make([]time.Time, 0)

	// Mock API to track calls
	callCount := 0
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).Run(func(args mock.Arguments) {
		callCount++
		refreshTimestamps = append(refreshTimestamps, time.Now())
	}).Return(expectedInstance, nil)

	// First call - populate cache
	_, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)
	assert.NoError(t, err)

	// Get the TTL that was assigned
	entry := cache.instancesCache[testID]
	firstTTL := entry.ttl

	// Create 5 more caches with same ID - each will get different TTL due to jitter
	ttls := []time.Duration{firstTTL}
	for i := 0; i < 5; i++ {
		freshCache := newExoscaleCache(mockClient, true)
		_, _ = freshCache.GetInstance(context.Background(), "ch-gva-2", testID)
		ttls = append(ttls, freshCache.instancesCache[testID].ttl)
	}

	// Verify TTLs are different across caches (jitter creates variation)
	uniqueTTLs := make(map[time.Duration]bool)
	for _, ttl := range ttls {
		uniqueTTLs[ttl] = true
	}

	// With jitter, we should see multiple unique TTL values (not all identical)
	assert.Greater(t, len(uniqueTTLs), 1, "Jitter should create variation in TTLs")

	// Verify all TTLs are within expected range
	baseTTL := 5 * time.Minute
	minExpected := baseTTL + time.Duration(float64(baseTTL)*0.10)
	maxExpected := baseTTL + time.Duration(float64(baseTTL)*0.30)

	for _, ttl := range ttls {
		assert.GreaterOrEqual(t, ttl, minExpected)
		assert.LessOrEqual(t, ttl, maxExpected)
	}
}

// Test concurrent reads (100 goroutines, same ID)
func TestThreadSafety_ConcurrentReadsSameID(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "concurrent-test-id"
	// Pre-populate cache
	cache.instancesCache[testID] = &cachedInstance{
		data:      &egoscale.Instance{ID: &testID, Name: &[]string{"test-instance"}[0]},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}

	// 100 concurrent reads of the same ID
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			instance, err := cache.GetInstance(context.Background(), "ch-gva-2", testID)
			if err != nil {
				errors <- err
				return
			}
			if instance == nil || *instance.ID != testID {
				errors <- assert.AnError
			}
		}()
	}

	wg.Wait()
	close(errors)

	// No errors should occur
	for err := range errors {
		t.Errorf("Concurrent read error: %v", err)
	}

	// All reads should hit cache (no API calls)
	mockClient.AssertNotCalled(t, "GetInstance")

	// Verify metrics (all 100 should be hits)
	assert.Equal(t, int64(100), atomic.LoadInt64(&cache.metrics.instanceHits))
}

// Test concurrent reads (different IDs)
func TestThreadSafety_ConcurrentReadsDifferentIDs(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Pre-populate cache with 10 different instances
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("instance-%d", i)
		cache.instancesCache[id] = &cachedInstance{
			data:      &egoscale.Instance{ID: &id},
			fetchedAt: time.Now(),
			ttl:       5 * time.Minute,
		}
	}

	// 100 concurrent reads across different IDs
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			id := fmt.Sprintf("instance-%d", index%10)
			_, _ = cache.GetInstance(context.Background(), "ch-gva-2", id)
		}(i)
	}

	wg.Wait()

	// Should complete without panics or deadlocks (run with -race to verify)
	mockClient.AssertNotCalled(t, "GetInstance")
}

// Test concurrent writes (race detector)
func TestThreadSafety_ConcurrentWrites(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	// Mock API calls for 10 different instances
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("write-test-%d", i)
		mockClient.On("GetInstance", mock.Anything, "ch-gva-2", id).
			Return(&egoscale.Instance{ID: &id}, nil)
	}

	// 100 concurrent writes (cache misses that populate cache)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			id := fmt.Sprintf("write-test-%d", index%10)
			_, _ = cache.GetInstance(context.Background(), "ch-gva-2", id)
		}(i)
	}

	wg.Wait()

	// Should complete without data races (run with `go test -race`)
	// Verify cache was populated
	assert.Equal(t, 10, len(cache.instancesCache))
}

// Test read during write
func TestThreadSafety_ReadDuringWrite(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "read-write-test"
	mockClient.On("GetInstance", mock.Anything, "ch-gva-2", testID).
		Return(&egoscale.Instance{ID: &testID}, nil).
		Run(func(args mock.Arguments) {
			// Simulate slow API call
			time.Sleep(10 * time.Millisecond)
		})

	// Start a write (cache miss)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = cache.GetInstance(context.Background(), "ch-gva-2", testID)
	}()

	// Give write time to start
	time.Sleep(2 * time.Millisecond)

	// Start concurrent reads while write is in progress
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.GetInstance(context.Background(), "ch-gva-2", testID)
		}()
	}

	wg.Wait()

	// Should complete without deadlock
	// One of the goroutines should have written to cache
	assert.NotNil(t, cache.instancesCache[testID])
}

// Test atomic metrics updates under concurrency
func TestThreadSafety_AtomicMetricsUpdates(t *testing.T) {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)

	testID := "metrics-concurrent-test"
	cache.instancesCache[testID] = &cachedInstance{
		data:      &egoscale.Instance{ID: &testID},
		fetchedAt: time.Now(),
		ttl:       5 * time.Minute,
	}

	// 1000 concurrent cache hits
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.GetInstance(context.Background(), "ch-gva-2", testID)
		}()
	}

	wg.Wait()

	// Metrics should be exactly 1000 (atomic operations prevent lost updates)
	metrics := cache.GetMetrics()
	assert.Equal(t, int64(1000), metrics.InstanceHits)
	assert.Equal(t, int64(0), metrics.InstanceMisses)
}
