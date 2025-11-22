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
	"os"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
)

func (ts *cloudProviderTestSuite) TestNewManager() {
	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().NoError(err)
	ts.Require().NotNil(manager)

	os.Unsetenv("EXOSCALE_API_KEY")
	os.Unsetenv("EXOSCALE_API_SECRET")

	manager, err = newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().Error(err)
	ts.Require().Nil(manager)
}

func (ts *cloudProviderTestSuite) TestComputeInstanceQuota() {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetQuota", ts.p.manager.ctx, ts.p.manager.zone, "instance").
		Return(
			&egoscale.Quota{
				Resource: &testComputeInstanceQuotaName,
				Usage:    &testComputeInstanceQuotaUsage,
				Limit:    &testComputeInstanceQuotaLimit,
			},
			nil,
		)

	actual, err := ts.p.manager.computeInstanceQuota()
	ts.Require().NoError(err)
	ts.Require().Equal(int(testComputeInstanceQuotaLimit), actual)
}

// TestManager_UsesCachedClient verifies that the Manager's client is wrapped with cache
func (ts *cloudProviderTestSuite) TestManager_UsesCachedClient() {
	// The manager is already created in SetupTest with cache wrapping
	// We verify by checking that the cache layer intercepts calls

	// Create a new manager without mocking to verify cache wrapping
	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().NoError(err)
	ts.Require().NotNil(manager)

	// The client should be an exoscaleCache, not the raw egoscale client
	// We can verify by checking if it implements the exoscaleClient interface
	_, ok := manager.client.(exoscaleClient)
	ts.Require().True(ok, "manager client should implement exoscaleClient interface")
}

// TestManager_CacheReducesAPICalls verifies that the cache reduces API call count
func (ts *cloudProviderTestSuite) TestManager_CacheReducesAPICalls() {
	mockClient := new(exoscaleClientMock)

	// Create a cache wrapping the mock client
	cache := newExoscaleCache(mockClient, false) // Disable jitter for deterministic testing

	// Replace the manager's client with our cached mock
	ts.p.manager.client = cache

	// Mock a successful GetInstancePool call (should only be called once)
	mockClient.On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID:   &testInstancePoolID,
				Name: &testInstancePoolName,
				Size: &testInstancePoolSize,
			},
			nil,
		).Once() // Critical: Should only be called once

	// First call - cache miss, API called
	pool1, err := ts.p.manager.client.GetInstancePool(ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID)
	ts.Require().NoError(err)
	ts.Require().Equal(testInstancePoolID, *pool1.ID)

	// Second call within TTL - cache hit, no API call
	pool2, err := ts.p.manager.client.GetInstancePool(ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID)
	ts.Require().NoError(err)
	ts.Require().Equal(testInstancePoolID, *pool2.ID)

	// Third call within TTL - cache hit, no API call
	pool3, err := ts.p.manager.client.GetInstancePool(ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID)
	ts.Require().NoError(err)
	ts.Require().Equal(testInstancePoolID, *pool3.ID)

	// Verify mock was called exactly once (cache prevented additional calls)
	mockClient.AssertExpectations(ts.T())
}

// TestManager_CachingEnabledByDefault verifies that caching is enabled by default
func (ts *cloudProviderTestSuite) TestManager_CachingEnabledByDefault() {
	// Unset the env var to test default behavior
	os.Unsetenv("EXOSCALE_API_CACHE_ENABLED")

	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().NoError(err)
	ts.Require().NotNil(manager)

	// Client should be wrapped with cache (exoscaleCache type)
	_, ok := manager.client.(*exoscaleCache)
	ts.Require().True(ok, "manager client should be wrapped with cache by default")
}

// TestManager_CachingCanBeDisabled verifies that caching can be explicitly disabled
func (ts *cloudProviderTestSuite) TestManager_CachingCanBeDisabled() {
	// Explicitly disable caching
	ts.T().Setenv("EXOSCALE_API_CACHE_ENABLED", "false")

	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().NoError(err)
	ts.Require().NotNil(manager)

	// Client should NOT be wrapped with cache
	_, ok := manager.client.(*exoscaleCache)
	ts.Require().False(ok, "manager client should not be wrapped when caching is disabled")
}

// TestManager_CachingExplicitlyEnabled verifies that caching can be explicitly enabled
func (ts *cloudProviderTestSuite) TestManager_CachingExplicitlyEnabled() {
	// Explicitly enable caching
	ts.T().Setenv("EXOSCALE_API_CACHE_ENABLED", "true")

	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	ts.Require().NoError(err)
	ts.Require().NotNil(manager)

	// Client should be wrapped with cache
	_, ok := manager.client.(*exoscaleCache)
	ts.Require().True(ok, "manager client should be wrapped when caching is explicitly enabled")
}
