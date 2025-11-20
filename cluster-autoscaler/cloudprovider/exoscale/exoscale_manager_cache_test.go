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
	"sync"
	"time"

	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
)

// Test helper functions to reduce duplication

// createMockInstance creates a standard mock instance for testing
func createMockInstance(id, name string) *egoscale.Instance {
	return &egoscale.Instance{
		ID:   &id,
		Name: &name,
		Manager: &egoscale.InstanceManager{
			ID:   testInstancePoolID,
			Type: "instance-pool",
		},
		State: &testInstanceState,
	}
}

// createMockInstancePool creates a standard mock instance pool for testing
func createMockInstancePool(id, name string) *egoscale.InstancePool {
	return &egoscale.InstancePool{
		ID:   &id,
		Name: &name,
		Size: &testInstancePoolSize,
	}
}

// createMockSKSCluster creates a standard mock SKS cluster for testing
func createMockSKSCluster(id, name string) *egoscale.SKSCluster {
	return &egoscale.SKSCluster{
		ID:   &id,
		Name: &name,
		Zone: &testZone,
	}
}

// setupInstanceAPICall sets up a mock API call expectation for GetInstance
func (ts *cloudProviderTestSuite) setupInstanceAPICall(instanceID string, mockInstance *egoscale.Instance) {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, instanceID).
		Return(mockInstance, nil).Once()
}

// setupInstancePoolAPICall sets up a mock API call expectation for GetInstancePool
func (ts *cloudProviderTestSuite) setupInstancePoolAPICall(instancePoolID string, mockInstancePool *egoscale.InstancePool) {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, instancePoolID).
		Return(mockInstancePool, nil).Once()
}

// Advanced test helper functions to eliminate duplication

// setupInstanceCacheEntry manually adds an instance to cache with specified TTL
func (ts *cloudProviderTestSuite) setupInstanceCacheEntry(instanceID string, instance *egoscale.Instance, ttl time.Duration) {
	ts.p.manager.instanceCacheMu.Lock()
	defer ts.p.manager.instanceCacheMu.Unlock()
	ts.p.manager.instanceCache[instanceID] = &instanceCacheEntry{
		instance:  instance,
		expiredAt: time.Now().Add(ttl),
	}
}

// setupInstancePoolCacheEntry manually adds an instance pool to cache with specified TTL
func (ts *cloudProviderTestSuite) setupInstancePoolCacheEntry(poolID string, pool *egoscale.InstancePool, ttl time.Duration) {
	ts.p.manager.instancePoolCacheMu.Lock()
	defer ts.p.manager.instancePoolCacheMu.Unlock()
	ts.p.manager.instancePoolCache[poolID] = &instancePoolCacheEntry{
		instancePool: pool,
		expiredAt:    time.Now().Add(ttl),
	}
}

// setupQuotaCacheEntry manually adds a quota to cache with specified TTL
func (ts *cloudProviderTestSuite) setupQuotaCacheEntry(quota int, ttl time.Duration) {
	ts.p.manager.quotaCacheMu.Lock()
	defer ts.p.manager.quotaCacheMu.Unlock()
	ts.p.manager.quotaCache = &quotaCacheEntry{
		quota:     quota,
		expiredAt: time.Now().Add(ttl),
	}
}

// verifyInstanceCacheExists checks if an instance exists in cache
func (ts *cloudProviderTestSuite) verifyInstanceCacheExists(instanceID string, shouldExist bool) {
	ts.p.manager.instanceCacheMu.RLock()
	defer ts.p.manager.instanceCacheMu.RUnlock()
	_, exists := ts.p.manager.instanceCache[instanceID]
	ts.Require().Equal(shouldExist, exists)
}

// verifyInstancePoolCacheExists checks if an instance pool exists in cache
func (ts *cloudProviderTestSuite) verifyInstancePoolCacheExists(poolID string, shouldExist bool) {
	ts.p.manager.instancePoolCacheMu.RLock()
	defer ts.p.manager.instancePoolCacheMu.RUnlock()
	_, exists := ts.p.manager.instancePoolCache[poolID]
	ts.Require().Equal(shouldExist, exists)
}

// verifyQuotaCacheExists checks if quota exists in cache
func (ts *cloudProviderTestSuite) verifyQuotaCacheExists(shouldExist bool) {
	ts.p.manager.quotaCacheMu.RLock()
	defer ts.p.manager.quotaCacheMu.RUnlock()
	exists := ts.p.manager.quotaCache != nil
	ts.Require().Equal(shouldExist, exists)
}

// assertMockExpectations is a helper to assert all mock expectations
func (ts *cloudProviderTestSuite) assertMockExpectations() {
	ts.p.manager.client.(*exoscaleClientMock).AssertExpectations(ts.T())
}

// testCacheMissHitPattern tests the common cache miss -> cache hit pattern
func (ts *cloudProviderTestSuite) testInstanceCacheMissHitPattern(instanceID string, mockInstance *egoscale.Instance) {
	// Test cache miss - should call API
	ts.setupInstanceAPICall(instanceID, mockInstance)

	instance1, err := ts.p.manager.GetInstanceCached(ts.p.manager.ctx, ts.p.manager.zone, instanceID)
	ts.Require().NoError(err)
	ts.Require().Equal(mockInstance, instance1)

	// Test cache hit - should NOT call API
	instance2, err := ts.p.manager.GetInstanceCached(ts.p.manager.ctx, ts.p.manager.zone, instanceID)
	ts.Require().NoError(err)
	ts.Require().Equal(mockInstance, instance2)

	ts.assertMockExpectations()
}

// testInstancePoolCacheMissHitPattern tests the common cache miss -> cache hit pattern for instance pools
func (ts *cloudProviderTestSuite) testInstancePoolCacheMissHitPattern(poolID string, mockPool *egoscale.InstancePool) {
	// Test cache miss - should call API
	ts.setupInstancePoolAPICall(poolID, mockPool)

	pool1, err := ts.p.manager.GetInstancePoolCached(ts.p.manager.ctx, ts.p.manager.zone, poolID)
	ts.Require().NoError(err)
	ts.Require().Equal(mockPool, pool1)

	// Test cache hit - should NOT call API
	pool2, err := ts.p.manager.GetInstancePoolCached(ts.p.manager.ctx, ts.p.manager.zone, poolID)
	ts.Require().NoError(err)
	ts.Require().Equal(mockPool, pool2)

	ts.assertMockExpectations()
}

func (ts *cloudProviderTestSuite) TestGetInstanceCached() {
	mockInstance := createMockInstance(testInstanceID, testInstanceName)
	ts.testInstanceCacheMissHitPattern(testInstanceID, mockInstance)
}

func (ts *cloudProviderTestSuite) TestGetInstancesCachedBatch() {
	instance1ID := testInstanceID
	instance2ID := testInstancePoolID // Reuse as second instance ID for simplicity

	mockInstance1 := createMockInstance(instance1ID, testInstanceName)
	mockInstance2 := createMockInstance(instance2ID, testInstancePoolName)

	// Both instances should be fetched from API (cache miss)
	ts.setupInstanceAPICall(instance1ID, mockInstance1)
	ts.setupInstanceAPICall(instance2ID, mockInstance2)

	instances, err := ts.p.manager.GetInstancesCached(ts.p.manager.ctx, ts.p.manager.zone, []string{instance1ID, instance2ID})
	ts.Require().NoError(err)
	ts.Require().Len(instances, 2)
	ts.Require().Equal(mockInstance1, instances[0])
	ts.Require().Equal(mockInstance2, instances[1])

	// Second call should use cache (no additional API calls)
	instances2, err := ts.p.manager.GetInstancesCached(ts.p.manager.ctx, ts.p.manager.zone, []string{instance1ID, instance2ID})
	ts.Require().NoError(err)
	ts.Require().Len(instances2, 2)
	ts.Require().Equal(mockInstance1, instances2[0])
	ts.Require().Equal(mockInstance2, instances2[1])

	ts.assertMockExpectations()
}

func (ts *cloudProviderTestSuite) TestGetInstancesCachedPartialCache() {
	instance1ID := testInstanceID
	instance2ID := testInstancePoolID // Reuse as second instance ID

	mockInstance1 := &egoscale.Instance{
		ID:    &instance1ID,
		Name:  &testInstanceName,
		State: &testInstanceState,
	}

	mockInstance2 := &egoscale.Instance{
		ID:    &instance2ID,
		Name:  &testInstancePoolName,
		State: &testInstanceState,
	}

	// Pre-populate cache with first instance
	ts.p.manager.instanceCacheMu.Lock()
	ts.p.manager.instanceCache[instance1ID] = &instanceCacheEntry{
		instance:  mockInstance1,
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.instanceCacheMu.Unlock()

	// Only second instance should be fetched from API
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, instance2ID).
		Return(mockInstance2, nil).Once()

	instances, err := ts.p.manager.GetInstancesCached(ts.p.manager.ctx, ts.p.manager.zone, []string{instance1ID, instance2ID})
	ts.Require().NoError(err)
	ts.Require().Len(instances, 2)
	ts.Require().Equal(mockInstance1, instances[0])
	ts.Require().Equal(mockInstance2, instances[1])

	ts.p.manager.client.(*exoscaleClientMock).AssertExpectations(ts.T())
}

func (ts *cloudProviderTestSuite) TestNodeGroupForNodeWithSKSNodepoolLabel() {
	// Create a mock SKS nodepool nodegroup
	sksNodepool := &egoscale.SKSNodepool{
		ID:             &testSKSNodepoolID,
		InstancePoolID: &testInstancePoolID,
		Name:           &testSKSNodepoolName,
		Size:           &testSKSNodepoolSize,
	}

	sksCluster := &egoscale.SKSCluster{
		ID:   &testSKSClusterID,
		Name: &testSKSClusterName,
	}

	nodeGroup := &sksNodepoolNodeGroup{
		sksNodepool: sksNodepool,
		sksCluster:  sksCluster,
		m:           ts.p.manager,
		minSize:     1,
		maxSize:     10,
	}

	// Add the nodegroup to manager's nodeGroups list
	ts.p.manager.nodeGroups = append(ts.p.manager.nodeGroups, nodeGroup)

	// Create a node with the nodepool-id label set to the SKS nodepool ID
	node := &apiv1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				"node.exoscale.net/nodepool-id": testSKSNodepoolID, // SKS nodepool ID, not instance pool ID
			},
		},
		Spec: apiv1.NodeSpec{
			ProviderID: "exoscale://" + testInstanceID,
		},
	}

	// This should use the fast path and NOT make any API calls
	foundNodeGroup, err := ts.p.NodeGroupForNode(node)
	ts.Require().NoError(err)
	ts.Require().Equal(nodeGroup, foundNodeGroup)

	// Verify no API calls were made (fast path was used)
	ts.p.manager.client.(*exoscaleClientMock).AssertNotCalled(ts.T(), "GetInstance")
	ts.p.manager.client.(*exoscaleClientMock).AssertNotCalled(ts.T(), "GetInstancePool")
	ts.p.manager.client.(*exoscaleClientMock).AssertNotCalled(ts.T(), "ListSKSClusters")
}

func (ts *cloudProviderTestSuite) TestGetInstanceCachedExpiration() {
	mockInstance := createMockInstance(testInstanceID, testInstanceName)

	// Manually set a cache entry that's already expired
	ts.setupInstanceCacheEntry(testInstanceID, mockInstance, -1*time.Minute) // Already expired

	// This should trigger API call because cache entry is expired
	ts.setupInstanceAPICall(testInstanceID, mockInstance)

	instance, err := ts.p.manager.GetInstanceCached(ts.p.manager.ctx, ts.p.manager.zone, testInstanceID)
	ts.Require().NoError(err)
	ts.Require().Equal(mockInstance, instance)

	ts.assertMockExpectations()
}

func (ts *cloudProviderTestSuite) TestInstanceCacheInvalidation() {
	mockInstance := createMockInstance(testInstanceID, testInstanceName)

	// Add entry to cache and verify it exists
	ts.setupInstanceCacheEntry(testInstanceID, mockInstance, 5*time.Minute)
	ts.verifyInstanceCacheExists(testInstanceID, true)

	// Invalidate cache and verify entry is removed
	ts.p.manager.InvalidateInstanceCache(testInstanceID)
	ts.verifyInstanceCacheExists(testInstanceID, false)
}

func (ts *cloudProviderTestSuite) TestInstanceCacheMultipleInvalidation() {
	mockInstance1 := &egoscale.Instance{ID: &testInstanceID}
	mockInstance2 := &egoscale.Instance{ID: &testInstancePoolID} // Reuse as second ID

	// Add entries to cache
	ts.p.manager.instanceCacheMu.Lock()
	ts.p.manager.instanceCache[testInstanceID] = &instanceCacheEntry{
		instance:  mockInstance1,
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.instanceCache[testInstancePoolID] = &instanceCacheEntry{
		instance:  mockInstance2,
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.instanceCacheMu.Unlock()

	// Invalidate multiple
	ts.p.manager.InvalidateInstanceCacheMultiple([]string{testInstanceID, testInstancePoolID})

	// Verify both entries are removed
	ts.p.manager.instanceCacheMu.RLock()
	ts.Require().Empty(ts.p.manager.instanceCache)
	ts.p.manager.instanceCacheMu.RUnlock()
}

func (ts *cloudProviderTestSuite) TestQuotaCaching() {
	mockQuota := &egoscale.Quota{
		Resource: &testComputeInstanceQuotaName,
		Usage:    &testComputeInstanceQuotaUsage,
		Limit:    &testComputeInstanceQuotaLimit,
	}

	// Test cache miss - should call API
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetQuota", ts.p.manager.ctx, ts.p.manager.zone, "instance").
		Return(mockQuota, nil).Once()

	quota1, err := ts.p.manager.computeInstanceQuota()
	ts.Require().NoError(err)
	ts.Require().Equal(int(testComputeInstanceQuotaLimit), quota1)

	// Test cache hit - should NOT call API
	quota2, err := ts.p.manager.computeInstanceQuota()
	ts.Require().NoError(err)
	ts.Require().Equal(int(testComputeInstanceQuotaLimit), quota2)

	ts.assertMockExpectations()
}

func (ts *cloudProviderTestSuite) TestQuotaCacheInvalidation() {
	// Set up cache entry
	ts.p.manager.quotaCacheMu.Lock()
	ts.p.manager.quotaCache = &quotaCacheEntry{
		quota:     int(testComputeInstanceQuotaLimit),
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.quotaCacheMu.Unlock()

	// Verify cache exists
	ts.p.manager.quotaCacheMu.RLock()
	ts.Require().NotNil(ts.p.manager.quotaCache)
	ts.p.manager.quotaCacheMu.RUnlock()

	// Invalidate
	ts.p.manager.InvalidateQuotaCache()

	// Verify cache is cleared
	ts.p.manager.quotaCacheMu.RLock()
	ts.Require().Nil(ts.p.manager.quotaCache)
	ts.p.manager.quotaCacheMu.RUnlock()
}

func (ts *cloudProviderTestSuite) TestSKSClustersCaching() {
	mockClusters := []*egoscale.SKSCluster{
		{
			ID:   &testSKSClusterID,
			Name: &testSKSClusterName,
		},
	}

	// Test cache miss - should call API
	ts.p.manager.client.(*exoscaleClientMock).
		On("ListSKSClusters", ts.p.manager.ctx, ts.p.manager.zone).
		Return(mockClusters, nil).Once()

	clusters1, err := ts.p.manager.ListSKSClustersCached(ts.p.manager.ctx, ts.p.manager.zone)
	ts.Require().NoError(err)
	ts.Require().Equal(mockClusters, clusters1)

	// Test cache hit - should NOT call API
	clusters2, err := ts.p.manager.ListSKSClustersCached(ts.p.manager.ctx, ts.p.manager.zone)
	ts.Require().NoError(err)
	ts.Require().Equal(mockClusters, clusters2)

	ts.p.manager.client.(*exoscaleClientMock).AssertExpectations(ts.T())
}

func (ts *cloudProviderTestSuite) TestSKSClustersCacheInvalidation() {
	mockClusters := []*egoscale.SKSCluster{{ID: &testSKSClusterID}}

	// Set up cache entry
	ts.p.manager.sksClustersCacheMu.Lock()
	ts.p.manager.sksClustersCache = &sksClustersCacheEntry{
		clusters:  mockClusters,
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.sksClustersCacheMu.Unlock()

	// Verify cache exists
	ts.p.manager.sksClustersCacheMu.RLock()
	ts.Require().NotNil(ts.p.manager.sksClustersCache)
	ts.p.manager.sksClustersCacheMu.RUnlock()

	// Invalidate
	ts.p.manager.InvalidateSKSClustersCache()

	// Verify cache is cleared
	ts.p.manager.sksClustersCacheMu.RLock()
	ts.Require().Nil(ts.p.manager.sksClustersCache)
	ts.p.manager.sksClustersCacheMu.RUnlock()
}

func (ts *cloudProviderTestSuite) TestGetInstancePoolCached() {
	mockInstancePool := createMockInstancePool(testInstancePoolID, testInstancePoolName)
	ts.testInstancePoolCacheMissHitPattern(testInstancePoolID, mockInstancePool)
}

func (ts *cloudProviderTestSuite) TestInstancePoolCacheInvalidation() {
	mockInstancePool := createMockInstancePool(testInstancePoolID, testInstancePoolName)

	// Add entry to cache and verify it exists
	ts.setupInstancePoolCacheEntry(testInstancePoolID, mockInstancePool, 5*time.Minute)
	ts.verifyInstancePoolCacheExists(testInstancePoolID, true)

	// Invalidate cache by ID and verify entry is removed
	ts.p.manager.InvalidateInstancePoolCacheByID(testInstancePoolID)
	ts.verifyInstancePoolCacheExists(testInstancePoolID, false)
}

func (ts *cloudProviderTestSuite) TestInstancePoolCacheInvalidationWithInstances() {
	mockInstancePool := &egoscale.InstancePool{
		ID:   &testInstancePoolID,
		Name: &testInstancePoolName,
	}

	mockInstance := &egoscale.Instance{
		ID:   &testInstanceID,
		Name: &testInstanceName,
		Manager: &egoscale.InstanceManager{
			ID:   testInstancePoolID,
			Type: "instance-pool",
		},
	}

	// Add instance pool and instance to cache
	ts.p.manager.instancePoolCacheMu.Lock()
	ts.p.manager.instancePoolCache[testInstancePoolID] = &instancePoolCacheEntry{
		instancePool: mockInstancePool,
		expiredAt:    time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.instancePoolCacheMu.Unlock()

	ts.p.manager.instanceCacheMu.Lock()
	ts.p.manager.instanceCache[testInstanceID] = &instanceCacheEntry{
		instance:  mockInstance,
		expiredAt: time.Now().Add(5 * time.Minute),
	}
	ts.p.manager.instanceCacheMu.Unlock()

	// Invalidate instance pool cache (should remove both pool and related instances)
	ts.p.manager.InvalidateInstancePoolCache(mockInstancePool)

	// Verify both entries are removed
	ts.p.manager.instancePoolCacheMu.RLock()
	_, poolExists := ts.p.manager.instancePoolCache[testInstancePoolID]
	ts.p.manager.instancePoolCacheMu.RUnlock()
	ts.Require().False(poolExists)

	ts.p.manager.instanceCacheMu.RLock()
	_, instanceExists := ts.p.manager.instanceCache[testInstanceID]
	ts.p.manager.instanceCacheMu.RUnlock()
	ts.Require().False(instanceExists)
}

func (ts *cloudProviderTestSuite) TestCacheExpiredCleanup() {
	// Add expired entries to all caches
	expiredTime := time.Now().Add(-1 * time.Minute)

	// Instance cache
	ts.p.manager.instanceCacheMu.Lock()
	ts.p.manager.instanceCache[testInstanceID] = &instanceCacheEntry{
		instance:  &egoscale.Instance{ID: &testInstanceID},
		expiredAt: expiredTime,
	}
	ts.p.manager.instanceCacheMu.Unlock()

	// Quota cache
	ts.p.manager.quotaCacheMu.Lock()
	ts.p.manager.quotaCache = &quotaCacheEntry{
		quota:     100,
		expiredAt: expiredTime,
	}
	ts.p.manager.quotaCacheMu.Unlock()

	// SKS clusters cache
	ts.p.manager.sksClustersCacheMu.Lock()
	ts.p.manager.sksClustersCache = &sksClustersCacheEntry{
		clusters:  []*egoscale.SKSCluster{{ID: &testSKSClusterID}},
		expiredAt: expiredTime,
	}
	ts.p.manager.sksClustersCacheMu.Unlock()

	// Instance pool cache
	ts.p.manager.instancePoolCacheMu.Lock()
	ts.p.manager.instancePoolCache[testInstancePoolID] = &instancePoolCacheEntry{
		instancePool: &egoscale.InstancePool{ID: &testInstancePoolID},
		expiredAt:    expiredTime,
	}
	ts.p.manager.instancePoolCacheMu.Unlock()

	// Run cleanup methods
	ts.p.manager.ClearExpiredInstanceCache()
	ts.p.manager.ClearExpiredQuotaCache()
	ts.p.manager.ClearExpiredSKSClustersCache()
	ts.p.manager.ClearExpiredInstancePoolCache()

	// Verify all expired entries are removed
	ts.p.manager.instanceCacheMu.RLock()
	ts.Require().Empty(ts.p.manager.instanceCache)
	ts.p.manager.instanceCacheMu.RUnlock()

	ts.p.manager.quotaCacheMu.RLock()
	ts.Require().Nil(ts.p.manager.quotaCache)
	ts.p.manager.quotaCacheMu.RUnlock()

	ts.p.manager.sksClustersCacheMu.RLock()
	ts.Require().Nil(ts.p.manager.sksClustersCache)
	ts.p.manager.sksClustersCacheMu.RUnlock()

	ts.p.manager.instancePoolCacheMu.RLock()
	ts.Require().Empty(ts.p.manager.instancePoolCache)
	ts.p.manager.instancePoolCacheMu.RUnlock()
}

func (ts *cloudProviderTestSuite) TestCacheConcurrency() {
	mockInstance := &egoscale.Instance{
		ID:    &testInstanceID,
		Name:  &testInstanceName,
		State: &testInstanceState,
	}

	// Setup mock to handle concurrent calls
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(mockInstance, nil)

	var wg sync.WaitGroup
	numGoroutines := 10
	results := make([]*egoscale.Instance, numGoroutines)

	// Test concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			instance, err := ts.p.manager.GetInstanceCached(ts.p.manager.ctx, ts.p.manager.zone, testInstanceID)
			ts.Require().NoError(err)
			results[index] = instance
		}(i)
	}

	wg.Wait()

	// Verify all goroutines got the same instance
	for i := 0; i < numGoroutines; i++ {
		ts.Require().Equal(mockInstance, results[i])
	}

	// The API should have been called at least once but not more than a few times
	// (due to potential race conditions during initial cache population)
	ts.p.manager.client.(*exoscaleClientMock).AssertExpectations(ts.T())
}

func (ts *cloudProviderTestSuite) TestJitterFunctionality() {
	// Test jitter for different TTL values
	testCases := []struct {
		name string
		ttl  time.Duration
	}{
		{"5 minutes", 5 * time.Minute},
		{"1 hour", 1 * time.Hour},
		{"10 minutes", 10 * time.Minute},
	}

	for _, tc := range testCases {
		ts.Run(tc.name, func() {
			// Test jitter multiple times to ensure it varies
			jitteredTTLs := make(map[time.Duration]bool)

			for i := 0; i < 10; i++ {
				jitteredTTL := ts.p.manager.addJitter(tc.ttl)
				jitteredTTLs[jitteredTTL] = true

				// Verify jitter is within expected bounds
				minExpected := tc.ttl + time.Duration(float64(tc.ttl.Nanoseconds())*jitterMinPercent/100)
				maxExpected := tc.ttl + time.Duration(float64(tc.ttl.Nanoseconds())*jitterMaxPercent/100)

				ts.Require().True(jitteredTTL >= minExpected && jitteredTTL <= maxExpected,
					"Jittered TTL %v is outside expected range [%v, %v]", jitteredTTL, minExpected, maxExpected)
			}

			// Verify that we got different values (jitter is working)
			ts.Require().GreaterOrEqual(len(jitteredTTLs), 2, "Jitter should produce different values across multiple calls")
		})
	}
}

func (ts *cloudProviderTestSuite) TestCacheJitterInPractice() {
	mockInstance := &egoscale.Instance{
		ID:   &testInstanceID,
		Name: &testInstanceName,
		Manager: &egoscale.InstanceManager{
			ID:   testInstancePoolID,
			Type: "instance-pool",
		},
		State: &testInstanceState,
	}

	// Mock the API call
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(mockInstance, nil).Once()

	// Create multiple instances with the same TTL and verify they have different expiration times
	var expirationTimes []time.Time

	for i := 0; i < 5; i++ {
		// Clear cache between calls to force new cache entries
		ts.p.manager.InvalidateInstanceCache(testInstanceID)

		// Call the cached method
		_, err := ts.p.manager.GetInstanceCached(ts.p.manager.ctx, ts.p.manager.zone, testInstanceID)
		ts.Require().NoError(err)

		// Get the expiration time from cache
		ts.p.manager.instanceCacheMu.RLock()
		if entry, exists := ts.p.manager.instanceCache[testInstanceID]; exists {
			expirationTimes = append(expirationTimes, entry.expiredAt)
		}
		ts.p.manager.instanceCacheMu.RUnlock()

		// Reset mock expectations for next iteration
		if i < 4 { // Don't add expectation for the last iteration
			ts.p.manager.client.(*exoscaleClientMock).
				On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
				Return(mockInstance, nil).Once()
		}
	}

	// Verify we have different expiration times (jitter is working)
	uniqueTimes := make(map[time.Time]bool)
	for _, expTime := range expirationTimes {
		uniqueTimes[expTime] = true
	}

	ts.Require().GreaterOrEqual(len(uniqueTimes), 2,
		"Cache entries should have different expiration times due to jitter")
}
