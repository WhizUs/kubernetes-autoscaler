/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package exoscale

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
	exoapi "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2/api"
)

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

const (
	defaultAPIEnvironment = "api"
	// Cache instances for 5 minutes to reduce API calls
	instanceCacheTTL = 5 * time.Minute
	// Cache quota for 1 hour since it rarely changes
	quotaCacheTTL = 1 * time.Hour
	// Cache SKS clusters for 10 minutes since they rarely change
	sksClustersCacheTTL = 10 * time.Minute
	// Cache instance pools for 5 minutes since they change less frequently than instances
	instancePoolCacheTTL = 5 * time.Minute

	// Jitter constants (as percentage of TTL)
	jitterMinPercent = 5  // 5% of TTL
	jitterMaxPercent = 20 // 20% of TTL
)

// Generic cache entry interface for consolidating cache operations
type cacheEntry interface {
	isExpired(now time.Time) bool
}

// Generic cache operations to reduce code duplication
func clearExpiredMapEntries[K comparable, V cacheEntry](cache map[K]V, mutex *sync.RWMutex) {
	now := time.Now()
	mutex.Lock()
	defer mutex.Unlock()
	for key, entry := range cache {
		if entry.isExpired(now) {
			delete(cache, key)
		}
	}
}

// instanceCacheEntry represents a cached instance with its expiration time
type instanceCacheEntry struct {
	instance  *egoscale.Instance
	expiredAt time.Time
}

func (e *instanceCacheEntry) isExpired(now time.Time) bool {
	return now.After(e.expiredAt)
}

// quotaCacheEntry represents a cached quota with its expiration time
type quotaCacheEntry struct {
	quota     int
	expiredAt time.Time
}

func (e *quotaCacheEntry) isExpired(now time.Time) bool {
	return now.After(e.expiredAt)
}

// sksClustersCacheEntry represents cached SKS clusters with expiration time
type sksClustersCacheEntry struct {
	clusters  []*egoscale.SKSCluster
	expiredAt time.Time
}

func (e *sksClustersCacheEntry) isExpired(now time.Time) bool {
	return now.After(e.expiredAt)
}

// instancePoolCacheEntry represents a cached instance pool with its expiration time
type instancePoolCacheEntry struct {
	instancePool *egoscale.InstancePool
	expiredAt    time.Time
}

func (e *instancePoolCacheEntry) isExpired(now time.Time) bool {
	return now.After(e.expiredAt)
}

// Manager handles Exoscale communication and data caching of
// node groups (Instance Pools).
type Manager struct {
	ctx           context.Context
	client        exoscaleClient
	zone          string
	nodeGroups    []cloudprovider.NodeGroup
	discoveryOpts cloudprovider.NodeGroupDiscoveryOptions

	// Instance cache to reduce API calls
	instanceCache   map[string]*instanceCacheEntry
	instanceCacheMu sync.RWMutex

	// Quota cache to reduce API calls
	quotaCache   *quotaCacheEntry
	quotaCacheMu sync.RWMutex

	// SKS clusters cache to reduce API calls
	sksClustersCache   *sksClustersCacheEntry
	sksClustersCacheMu sync.RWMutex

	// Instance pool cache to reduce API calls
	instancePoolCache   map[string]*instancePoolCacheEntry
	instancePoolCacheMu sync.RWMutex
}

func newManager(discoveryOpts cloudprovider.NodeGroupDiscoveryOptions) (*Manager, error) {
	var (
		zone           string
		apiKey         string
		apiSecret      string
		apiEnvironment string
		err            error
	)

	if zone = os.Getenv("EXOSCALE_ZONE"); zone == "" {
		return nil, errors.New("no Exoscale zone specified")
	}

	if apiKey = os.Getenv("EXOSCALE_API_KEY"); apiKey == "" {
		return nil, errors.New("no Exoscale API key specified")
	}

	if apiSecret = os.Getenv("EXOSCALE_API_SECRET"); apiSecret == "" {
		return nil, errors.New("no Exoscale API secret specified")
	}

	if apiEnvironment = os.Getenv("EXOSCALE_API_ENVIRONMENT"); apiEnvironment == "" {
		apiEnvironment = defaultAPIEnvironment
	}

	client, err := egoscale.NewClient(apiKey, apiSecret)
	if err != nil {
		return nil, err
	}

	debugf("initializing manager with zone=%s environment=%s", zone, apiEnvironment)

	m := &Manager{
		ctx:               exoapi.WithEndpoint(context.Background(), exoapi.NewReqEndpoint(apiEnvironment, zone)),
		client:            client,
		zone:              zone,
		discoveryOpts:     discoveryOpts,
		instanceCache:     make(map[string]*instanceCacheEntry),
		instancePoolCache: make(map[string]*instancePoolCacheEntry),
	}

	return m, nil
}

// Refresh refreshes the cache holding the node groups. This is called by the CA
// based on the `--scan-interval`. By default it's 10 seconds.
func (m *Manager) Refresh() error {
	// Clean up expired cache entries
	m.ClearExpiredInstanceCache()
	m.ClearExpiredQuotaCache()
	m.ClearExpiredSKSClustersCache()
	m.ClearExpiredInstancePoolCache()

	var nodeGroups []cloudprovider.NodeGroup
	for _, ng := range m.nodeGroups {
		if _, err := m.GetInstancePoolCached(m.ctx, m.zone, ng.Id()); err != nil {
			if errors.Is(err, exoapi.ErrNotFound) {
				debugf("removing node group %s from manager cache", ng.Id())
				continue
			}
			errorf("unable to retrieve Instance Pool %s: %v", ng.Id(), err)
			return err
		}

		nodeGroups = append(nodeGroups, ng)
	}
	m.nodeGroups = nodeGroups

	if len(m.nodeGroups) == 0 {
		infof("cluster-autoscaler is disabled: no node groups found")
	}

	return nil
}

// addJitter adds random jitter to TTL to prevent thundering herd.
// Jitter is between 5% and 20% of the original TTL.
func (m *Manager) addJitter(ttl time.Duration) time.Duration {
	minJitter := int64(float64(ttl.Nanoseconds()) * jitterMinPercent / 100)
	maxJitter := int64(float64(ttl.Nanoseconds()) * jitterMaxPercent / 100)
	jitter := time.Duration(rand.Int63n(maxJitter-minJitter) + minJitter)
	return ttl + jitter
}

// logCacheHit logs when a cache entry is found and returned
func (m *Manager) logCacheHit(cacheType, identifier string) {
	debugf("returning cached %s %s", cacheType, identifier)
}

// logCacheMiss logs when a cache miss occurs and API call is needed
func (m *Manager) logCacheMiss(cacheType, identifier string) {
	debugf("fetching %s %s from API", cacheType, identifier)
}

// invalidateCacheEntry is a generic helper for cache invalidation with logging
func (m *Manager) invalidateCacheEntry(cacheType, identifier string) {
	debugf("invalidating %s cache for %s", cacheType, identifier)
}

func (m *Manager) computeInstanceQuota() (int, error) {
	now := time.Now()

	// Check cache first
	m.quotaCacheMu.RLock()
	if m.quotaCache != nil && now.Before(m.quotaCache.expiredAt) {
		cachedQuota := m.quotaCache.quota
		m.quotaCacheMu.RUnlock()
		debugf("cache hit: returning cached instance quota %d", cachedQuota)
		return cachedQuota, nil
	}
	m.quotaCacheMu.RUnlock()

	// Cache miss or expired, fetch from API
	m.logCacheMiss("instance", "quota")
	instanceQuota, err := m.client.GetQuota(m.ctx, m.zone, "instance")
	if err != nil {
		return 0, fmt.Errorf("unable to retrieve Compute instances quota: %v", err)
	}

	quotaValue := int(*instanceQuota.Limit)

	// Cache the result
	m.quotaCacheMu.Lock()
	m.quotaCache = &quotaCacheEntry{
		quota:     quotaValue,
		expiredAt: now.Add(m.addJitter(quotaCacheTTL)),
	}
	m.quotaCacheMu.Unlock()

	return quotaValue, nil
}

// GetInstanceCached retrieves an instance from cache if available and not expired,
// otherwise fetches it from the API and caches it.
func (m *Manager) GetInstanceCached(ctx context.Context, zone, instanceID string) (*egoscale.Instance, error) {
	now := time.Now()

	// Check cache first
	m.instanceCacheMu.RLock()
	if entry, exists := m.instanceCache[instanceID]; exists && !entry.isExpired(now) {
		m.instanceCacheMu.RUnlock()
		m.logCacheHit("instance", instanceID)
		return entry.instance, nil
	}
	m.instanceCacheMu.RUnlock()

	// Cache miss or expired, fetch from API
	m.logCacheMiss("instance", instanceID)
	instance, err := m.client.GetInstance(ctx, zone, instanceID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	ttlWithJitter := m.addJitter(instanceCacheTTL)
	m.instanceCacheMu.Lock()
	m.instanceCache[instanceID] = &instanceCacheEntry{
		instance:  instance,
		expiredAt: now.Add(ttlWithJitter),
	}
	m.instanceCacheMu.Unlock()
	debugf("cached instance %s for %v (with jitter)", instanceID, ttlWithJitter)

	return instance, nil
}

// GetInstancesCached retrieves multiple instances efficiently, using cache when possible.
// This is optimized for batch operations where we know all instances belong to the same nodepool.
func (m *Manager) GetInstancesCached(ctx context.Context, zone string, instanceIDs []string) ([]*egoscale.Instance, error) {
	now := time.Now()
	instances := make([]*egoscale.Instance, len(instanceIDs))
	var missingIDs []string
	var missingIndices []int

	// First pass: collect cached instances and identify missing ones
	m.instanceCacheMu.RLock()
	for i, instanceID := range instanceIDs {
		if entry, exists := m.instanceCache[instanceID]; exists && now.Before(entry.expiredAt) {
			instances[i] = entry.instance
			m.logCacheHit("instance", instanceID)
		} else {
			missingIDs = append(missingIDs, instanceID)
			missingIndices = append(missingIndices, i)
		}
	}
	m.instanceCacheMu.RUnlock()

	// Second pass: fetch missing instances from API
	if len(missingIDs) > 0 {
		m.logCacheMiss("instances", fmt.Sprintf("%d instances: %v", len(missingIDs), missingIDs))

		// Fetch missing instances concurrently for better performance
		type fetchResult struct {
			index    int
			instance *egoscale.Instance
			err      error
		}

		results := make(chan fetchResult, len(missingIDs))

		for i, instanceID := range missingIDs {
			go func(idx int, id string) {
				instance, err := m.client.GetInstance(ctx, zone, id)
				results <- fetchResult{index: idx, instance: instance, err: err}
			}(i, instanceID)
		}

		// Collect results and cache them
		for i := 0; i < len(missingIDs); i++ {
			result := <-results
			if result.err != nil {
				return nil, result.err
			}

			originalIndex := missingIndices[result.index]
			instances[originalIndex] = result.instance

			// Cache the result
			ttlWithJitter := m.addJitter(instanceCacheTTL)
			instanceID := missingIDs[result.index]
			m.instanceCacheMu.Lock()
			m.instanceCache[instanceID] = &instanceCacheEntry{
				instance:  result.instance,
				expiredAt: now.Add(ttlWithJitter),
			}
			m.instanceCacheMu.Unlock()
			debugf("cached instance %s for %v (batch operation)", instanceID, ttlWithJitter)
		}
	}

	return instances, nil
}

// InvalidateInstanceCache removes an instance from the cache.
// This should be called when an instance is known to have changed (e.g., after scaling operations).
func (m *Manager) InvalidateInstanceCache(instanceID string) {
	m.instanceCacheMu.Lock()
	delete(m.instanceCache, instanceID)
	m.instanceCacheMu.Unlock()
	debugf("invalidated cache for instance %s", instanceID)
}

// InvalidateInstanceCacheMultiple removes multiple instances from the cache.
func (m *Manager) InvalidateInstanceCacheMultiple(instanceIDs []string) {
	m.instanceCacheMu.Lock()
	for _, instanceID := range instanceIDs {
		delete(m.instanceCache, instanceID)
	}
	m.instanceCacheMu.Unlock()
	debugf("invalidated cache for instances %v", instanceIDs)
}

// InvalidateInstancePoolCache removes all instances from the cache that belong to the given instance pool
func (m *Manager) InvalidateInstancePoolCache(instancePool *egoscale.InstancePool) {
	if instancePool == nil || instancePool.ID == nil {
		return
	}

	poolID := *instancePool.ID

	// We need to get the instances in this pool to invalidate their cache entries
	// Since we can't directly access the instances from the pool, we'll just invalidate by pool ID
	m.instancePoolCacheMu.Lock()
	delete(m.instancePoolCache, poolID)
	m.instancePoolCacheMu.Unlock()

	// Also try to invalidate any cached instances that might belong to this pool
	// We'll need to check each cached instance to see if it belongs to this pool
	m.instanceCacheMu.Lock()
	for instanceID, entry := range m.instanceCache {
		if entry.instance.Manager != nil && entry.instance.Manager.ID == poolID {
			delete(m.instanceCache, instanceID)
		}
	}
	m.instanceCacheMu.Unlock()
}

// ClearExpiredInstanceCache removes expired entries from the cache.
// This is called during Refresh to prevent unbounded cache growth.
func (m *Manager) ClearExpiredInstanceCache() {
	m.instanceCacheMu.RLock()
	initialCount := len(m.instanceCache)
	m.instanceCacheMu.RUnlock()

	clearExpiredMapEntries(m.instanceCache, &m.instanceCacheMu)

	m.instanceCacheMu.RLock()
	finalCount := len(m.instanceCache)
	m.instanceCacheMu.RUnlock()

	if removed := initialCount - finalCount; removed > 0 {
		debugf("cleared %d expired instance cache entries (%d remaining)", removed, finalCount)
	}
}

// ClearExpiredQuotaCache removes expired quota cache entry.
// This is called during Refresh to prevent stale quota data.
func (m *Manager) ClearExpiredQuotaCache() {
	now := time.Now()
	m.quotaCacheMu.Lock()
	if m.quotaCache != nil && now.After(m.quotaCache.expiredAt) {
		m.quotaCache = nil
		debugf("cleared expired quota cache")
	}
	m.quotaCacheMu.Unlock()
}

// InvalidateQuotaCache removes the quota from the cache.
// This can be called if the quota is known to have changed.
func (m *Manager) InvalidateQuotaCache() {
	m.quotaCacheMu.Lock()
	m.quotaCache = nil
	m.quotaCacheMu.Unlock()
	debugf("invalidated quota cache")
}

// ListSKSClustersCached retrieves SKS clusters from cache if available and not expired,
// otherwise fetches them from the API and caches them.
func (m *Manager) ListSKSClustersCached(ctx context.Context, zone string) ([]*egoscale.SKSCluster, error) {
	now := time.Now()

	// Check cache first
	m.sksClustersCacheMu.RLock()
	if m.sksClustersCache != nil && now.Before(m.sksClustersCache.expiredAt) {
		cachedClusters := m.sksClustersCache.clusters
		m.sksClustersCacheMu.RUnlock()
		m.logCacheHit("SKS clusters", fmt.Sprintf("(%d clusters)", len(cachedClusters)))
		return cachedClusters, nil
	}
	m.sksClustersCacheMu.RUnlock()

	// Cache miss or expired, fetch from API
	m.logCacheMiss("SKS", "clusters")
	clusters, err := m.client.ListSKSClusters(ctx, zone)
	if err != nil {
		return nil, err
	}

	// Cache the result
	m.sksClustersCacheMu.Lock()
	m.sksClustersCache = &sksClustersCacheEntry{
		clusters:  clusters,
		expiredAt: now.Add(m.addJitter(sksClustersCacheTTL)),
	}
	m.sksClustersCacheMu.Unlock()

	return clusters, nil
}

// InvalidateSKSClustersCache removes the SKS clusters from the cache.
// This can be called if the clusters are known to have changed.
func (m *Manager) InvalidateSKSClustersCache() {
	m.sksClustersCacheMu.Lock()
	m.sksClustersCache = nil
	m.sksClustersCacheMu.Unlock()
	debugf("invalidated SKS clusters cache")
}

// ClearExpiredSKSClustersCache removes expired SKS clusters cache entry.
// This is called during Refresh to prevent stale cluster data.
func (m *Manager) ClearExpiredSKSClustersCache() {
	now := time.Now()
	m.sksClustersCacheMu.Lock()
	if m.sksClustersCache != nil && now.After(m.sksClustersCache.expiredAt) {
		m.sksClustersCache = nil
		debugf("cleared expired SKS clusters cache")
	}
	m.sksClustersCacheMu.Unlock()
}

// GetInstancePoolCached retrieves an instance pool from cache if available and not expired,
// otherwise fetches it from the API and caches it.
func (m *Manager) GetInstancePoolCached(ctx context.Context, zone, instancePoolID string) (*egoscale.InstancePool, error) {
	now := time.Now()

	// Check cache first
	m.instancePoolCacheMu.RLock()
	if entry, exists := m.instancePoolCache[instancePoolID]; exists && !entry.isExpired(now) {
		m.instancePoolCacheMu.RUnlock()
		m.logCacheHit("instance pool", instancePoolID)
		return entry.instancePool, nil
	}
	m.instancePoolCacheMu.RUnlock()

	// Cache miss or expired, fetch from API
	m.logCacheMiss("instance pool", instancePoolID)
	instancePool, err := m.client.GetInstancePool(ctx, zone, instancePoolID)
	if err != nil {
		return nil, err
	}

	// Cache the result
	m.instancePoolCacheMu.Lock()
	m.instancePoolCache[instancePoolID] = &instancePoolCacheEntry{
		instancePool: instancePool,
		expiredAt:    now.Add(m.addJitter(instancePoolCacheTTL)),
	}
	m.instancePoolCacheMu.Unlock()

	return instancePool, nil
}

// InvalidateInstancePoolCacheByID removes an instance pool from the cache by ID.
// This should be called when an instance pool is known to have changed.
func (m *Manager) InvalidateInstancePoolCacheByID(instancePoolID string) {
	m.instancePoolCacheMu.Lock()
	delete(m.instancePoolCache, instancePoolID)
	m.instancePoolCacheMu.Unlock()
	debugf("invalidated cache for instance pool %s", instancePoolID)
}

// ClearExpiredInstancePoolCache removes expired entries from the instance pool cache.
// This is called during Refresh to prevent unbounded cache growth.
func (m *Manager) ClearExpiredInstancePoolCache() {
	clearExpiredMapEntries(m.instancePoolCache, &m.instancePoolCacheMu)
}
