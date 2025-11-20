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
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	egoscale "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/exoscale/internal/github.com/exoscale/egoscale/v2"
)

var (
	testComputeInstanceQuotaLimit int64 = 20
	testComputeInstanceQuotaName        = "instance"
	testComputeInstanceQuotaUsage int64 = 4
	testInstanceID                      = new(cloudProviderTestSuite).randomID()
	testInstanceName                    = new(cloudProviderTestSuite).randomString(10)
	testInstancePoolID                  = new(cloudProviderTestSuite).randomID()
	testInstancePoolName                = new(cloudProviderTestSuite).randomString(10)
	testInstancePoolSize          int64 = 1
	testInstancePoolState               = "running"
	testInstanceState                   = "running"
	testSKSClusterID                    = new(cloudProviderTestSuite).randomID()
	testSKSClusterName                  = new(cloudProviderTestSuite).randomString(10)
	testSKSNodepoolID                   = new(cloudProviderTestSuite).randomID()
	testSKSNodepoolName                 = new(cloudProviderTestSuite).randomString(10)
	testSKSNodepoolSize           int64 = 1
	testSeededRand                      = rand.New(rand.NewSource(time.Now().UnixNano()))
	testZone                            = "ch-gva-2"
)

type exoscaleClientMock struct {
	mock.Mock
}

func (m *exoscaleClientMock) EvictInstancePoolMembers(
	ctx context.Context,
	zone string,
	instancePool *egoscale.InstancePool,
	members []string,
) error {
	args := m.Called(ctx, zone, instancePool, members)
	return args.Error(0)
}

func (m *exoscaleClientMock) EvictSKSNodepoolMembers(
	ctx context.Context,
	zone string,
	cluster *egoscale.SKSCluster,
	nodepool *egoscale.SKSNodepool,
	members []string,
) error {
	args := m.Called(ctx, zone, cluster, nodepool, members)
	return args.Error(0)
}

func (m *exoscaleClientMock) GetInstance(ctx context.Context, zone, id string) (*egoscale.Instance, error) {
	args := m.Called(ctx, zone, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egoscale.Instance), args.Error(1)
}

func (m *exoscaleClientMock) GetInstancePool(ctx context.Context, zone, id string) (*egoscale.InstancePool, error) {
	args := m.Called(ctx, zone, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egoscale.InstancePool), args.Error(1)
}

func (m *exoscaleClientMock) GetQuota(ctx context.Context, zone string, resource string) (*egoscale.Quota, error) {
	args := m.Called(ctx, zone, resource)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*egoscale.Quota), args.Error(1)
}

func (m *exoscaleClientMock) ListSKSClusters(ctx context.Context, zone string) ([]*egoscale.SKSCluster, error) {
	args := m.Called(ctx, zone)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*egoscale.SKSCluster), args.Error(1)
}

func (m *exoscaleClientMock) ScaleInstancePool(
	ctx context.Context,
	zone string,
	instancePool *egoscale.InstancePool,
	size int64,
) error {
	args := m.Called(ctx, zone, instancePool, size)
	return args.Error(0)
}

func (m *exoscaleClientMock) ScaleSKSNodepool(
	ctx context.Context,
	zone string,
	cluster *egoscale.SKSCluster,
	nodepool *egoscale.SKSNodepool,
	size int64,
) error {
	args := m.Called(ctx, zone, cluster, nodepool, size)
	return args.Error(0)
}

type cloudProviderTestSuite struct {
	p *exoscaleCloudProvider

	suite.Suite
}

func (ts *cloudProviderTestSuite) SetupTest() {
	ts.T().Setenv("EXOSCALE_ZONE", testZone)
	ts.T().Setenv("EXOSCALE_API_KEY", "x")
	ts.T().Setenv("EXOSCALE_API_SECRET", "x")

	manager, err := newManager(cloudprovider.NodeGroupDiscoveryOptions{})
	if err != nil {
		ts.T().Fatalf("error initializing cloud provider manager: %v", err)
	}
	manager.client = new(exoscaleClientMock)

	provider, err := newExoscaleCloudProvider(manager, &cloudprovider.ResourceLimiter{})
	if err != nil {
		ts.T().Fatalf("error initializing cloud provider: %v", err)
	}

	ts.p = provider
}

func (ts *cloudProviderTestSuite) TearDownTest() {
}

func (ts *cloudProviderTestSuite) randomID() string {
	id := uuid.New()
	return id.String()
}

func (ts *cloudProviderTestSuite) randomStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[testSeededRand.Intn(len(charset))]
	}
	return string(b)
}

func (ts *cloudProviderTestSuite) randomString(length int) string {
	const defaultCharset = "abcdefghijklmnopqrstuvwxyz" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	return ts.randomStringWithCharset(length, defaultCharset)
}

func (ts *cloudProviderTestSuite) TestExoscaleCloudProvider_Name() {
	ts.Require().Equal(cloudprovider.ExoscaleProviderName, ts.p.Name())
}

func (ts *cloudProviderTestSuite) TestExoscaleCloudProvider_NodeGroupForNode_InstancePool() {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID:   &testInstancePoolID,
				Name: &testInstancePoolName,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
				Manager: &egoscale.InstanceManager{
					ID:   testInstancePoolID,
					Type: "instance-pool",
				},
			},
			nil,
		)

	nodeGroup, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(testInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(nodeGroup)
	ts.Require().Equal(testInstancePoolID, nodeGroup.Id())
	ts.Require().IsType(&instancePoolNodeGroup{}, nodeGroup)
}

func (ts *cloudProviderTestSuite) TestExoscaleCloudProvider_NodeGroupForNode_SKSNodepool() {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetQuota", ts.p.manager.ctx, ts.p.manager.zone, testComputeInstanceQuotaName).
		Return(
			&egoscale.Quota{
				Resource: &testComputeInstanceQuotaName,
				Usage:    &testComputeInstanceQuotaUsage,
				Limit:    &testComputeInstanceQuotaLimit,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("ListSKSClusters", ts.p.manager.ctx, ts.p.manager.zone).
		Return(
			[]*egoscale.SKSCluster{{
				ID:   &testSKSClusterID,
				Name: &testSKSClusterName,
				Nodepools: []*egoscale.SKSNodepool{{
					ID:             &testSKSNodepoolID,
					InstancePoolID: &testInstancePoolID,
					Name:           &testSKSNodepoolName,
				}},
			}},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID: &testInstancePoolID,
				Manager: &egoscale.InstancePoolManager{
					ID:   testSKSNodepoolID,
					Type: "sks-nodepool",
				},
				Name: &testInstancePoolName,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
				Manager: &egoscale.InstanceManager{
					ID:   testInstancePoolID,
					Type: "instance-pool",
				},
			},
			nil,
		)

	nodeGroup, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(testInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(nodeGroup)
	ts.Require().Equal(testInstancePoolID, nodeGroup.Id())
	ts.Require().IsType(&sksNodepoolNodeGroup{}, nodeGroup)
}

func (ts *cloudProviderTestSuite) TestExoscaleCloudProvider_NodeGroupForNode_Standalone() {
	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
			},
			nil,
		)

	nodeGroup, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(testInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().Nil(nodeGroup)
}

func (ts *cloudProviderTestSuite) TestExoscaleCloudProvider_NodeGroups() {
	var (
		instancePoolID              = ts.randomID()
		instancePoolName            = ts.randomString(10)
		instancePoolInstanceID      = ts.randomID()
		sksNodepoolInstanceID       = ts.randomID()
		sksNodepoolInstancePoolID   = ts.randomID()
		sksNodepoolInstancePoolName = ts.randomString(10)
	)

	// In order to test the caching system of the cloud provider manager,
	// we mock 1 Instance Pool based Nodegroup and 1 SKS Nodepool based
	// Nodegroup. If everything works as expected, the
	// cloudprovider.NodeGroups() method should return 2 Nodegroups.

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetQuota", ts.p.manager.ctx, ts.p.manager.zone, testComputeInstanceQuotaName).
		Return(
			&egoscale.Quota{
				Resource: &testComputeInstanceQuotaName,
				Usage:    &testComputeInstanceQuotaUsage,
				Limit:    &testComputeInstanceQuotaLimit,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, instancePoolID).
		Return(
			&egoscale.InstancePool{
				ID:   &instancePoolID,
				Name: &instancePoolName,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, instancePoolInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
				Manager: &egoscale.InstanceManager{
					ID:   instancePoolID,
					Type: "instance-pool",
				},
			},
			nil,
		)

	instancePoolNodeGroup, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(instancePoolInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(instancePoolNodeGroup)

	// ---------------------------------------------------------------

	ts.p.manager.client.(*exoscaleClientMock).
		On("ListSKSClusters", ts.p.manager.ctx, ts.p.manager.zone).
		Return(
			[]*egoscale.SKSCluster{{
				ID:   &testSKSClusterID,
				Name: &testSKSClusterName,
				Nodepools: []*egoscale.SKSNodepool{{
					ID:             &testSKSNodepoolID,
					InstancePoolID: &sksNodepoolInstancePoolID,
					Name:           &testSKSNodepoolName,
				}},
			}},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, sksNodepoolInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID: &sksNodepoolInstancePoolID,
				Manager: &egoscale.InstancePoolManager{
					ID:   testSKSNodepoolID,
					Type: "sks-nodepool",
				},
				Name: &sksNodepoolInstancePoolName,
			},
			nil,
		)

	ts.p.manager.client.(*exoscaleClientMock).
		On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, sksNodepoolInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
				Manager: &egoscale.InstanceManager{
					ID:   sksNodepoolInstancePoolID,
					Type: "instance-pool",
				},
			},
			nil,
		)

	sksNodepoolNodeGroup, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(sksNodepoolInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(sksNodepoolNodeGroup)

	// ---------------------------------------------------------------

	ts.Require().Len(ts.p.NodeGroups(), 2)
}

// TestCache_NodeGroupForNode_CachesSKSClusters verifies that NodeGroupForNode uses cached SKS clusters
func (ts *cloudProviderTestSuite) TestCache_NodeGroupForNode_CachesSKSClusters() {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false) // Disable jitter for deterministic testing
	ts.p.manager.client = cache

	// Mock ListSKSClusters - should only be called once due to caching
	mockClient.On("ListSKSClusters", ts.p.manager.ctx, ts.p.manager.zone).
		Return(
			[]*egoscale.SKSCluster{{
				ID:   &testSKSClusterID,
				Name: &testSKSClusterName,
				Nodepools: []*egoscale.SKSNodepool{{
					ID:             &testSKSNodepoolID,
					InstancePoolID: &testInstancePoolID,
					Name:           &testSKSNodepoolName,
				}},
			}},
			nil,
		).Once()

	mockClient.On("GetQuota", ts.p.manager.ctx, ts.p.manager.zone, testComputeInstanceQuotaName).
		Return(
			&egoscale.Quota{
				Resource: &testComputeInstanceQuotaName,
				Usage:    &testComputeInstanceQuotaUsage,
				Limit:    &testComputeInstanceQuotaLimit,
			},
			nil,
		).Once()

	mockClient.On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID: &testInstancePoolID,
				Manager: &egoscale.InstancePoolManager{
					ID:   testSKSNodepoolID,
					Type: "sks-nodepool",
				},
				Name: &testInstancePoolName,
			},
			nil,
		).Once()

	mockClient.On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:   &testInstanceID,
				Name: &testInstanceName,
				Manager: &egoscale.InstanceManager{
					ID:   testInstancePoolID,
					Type: "instance-pool",
				},
			},
			nil,
		).Once()

	// First call - populates cache
	nodeGroup1, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(testInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(nodeGroup1)

	// Second call within TTL - uses cache, no additional API calls
	nodeGroup2, err := ts.p.NodeGroupForNode(&apiv1.Node{
		Spec: apiv1.NodeSpec{
			ProviderID: toProviderID(testInstanceID),
		},
		ObjectMeta: v1.ObjectMeta{
			Labels: map[string]string{
				"topology.kubernetes.io/region": testZone,
			},
		},
	})
	ts.Require().NoError(err)
	ts.Require().NotNil(nodeGroup2)

	// Verify all mocks were called exactly the expected number of times
	mockClient.AssertExpectations(ts.T())
}

// TestCache_Nodes_UsesCachedInstances verifies that Nodes() uses cached instances
func (ts *cloudProviderTestSuite) TestCache_Nodes_UsesCachedInstances() {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)
	ts.p.manager.client = cache

	// Create an instance pool node group
	instanceIDs := []string{testInstanceID}
	ng := &instancePoolNodeGroup{
		m: ts.p.manager,
		instancePool: &egoscale.InstancePool{
			ID:          &testInstancePoolID,
			Name:        &testInstancePoolName,
			InstanceIDs: &instanceIDs,
		},
	}

	// Mock GetInstance - should only be called once due to caching
	mockClient.On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:    &testInstanceID,
				Name:  &testInstanceName,
				State: &testInstanceState,
			},
			nil,
		).Once()

	// First call - cache miss
	nodes1, err := ng.Nodes()
	ts.Require().NoError(err)
	ts.Require().Len(nodes1, 1)

	// Second call within TTL - cache hit
	nodes2, err := ng.Nodes()
	ts.Require().NoError(err)
	ts.Require().Len(nodes2, 1)

	// Verify GetInstance was only called once
	mockClient.AssertExpectations(ts.T())
}

// TestCache_Refresh_BenefitsFromCache verifies that Refresh() benefits from cached pools
func (ts *cloudProviderTestSuite) TestCache_Refresh_BenefitsFromCache() {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)
	ts.p.manager.client = cache

	// Add a node group to the manager
	ts.p.manager.nodeGroups = []cloudprovider.NodeGroup{
		&instancePoolNodeGroup{
			m: ts.p.manager,
			instancePool: &egoscale.InstancePool{
				ID:   &testInstancePoolID,
				Name: &testInstancePoolName,
			},
		},
	}

	// Mock GetInstancePool - should only be called once due to caching
	mockClient.On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID:   &testInstancePoolID,
				Name: &testInstancePoolName,
			},
			nil,
		).Once()

	// First refresh - cache miss
	err := ts.p.manager.Refresh()
	ts.Require().NoError(err)
	ts.Require().Len(ts.p.manager.nodeGroups, 1)

	// Second refresh within TTL - cache hit, no API call
	err = ts.p.manager.Refresh()
	ts.Require().NoError(err)
	ts.Require().Len(ts.p.manager.nodeGroups, 1)

	// Verify GetInstancePool was only called once
	mockClient.AssertExpectations(ts.T())
}

// TestCache_ConsecutiveLoops_ZeroAPICalls verifies consecutive autoscaler loops use only cache
func (ts *cloudProviderTestSuite) TestCache_ConsecutiveLoops_ZeroAPICalls() {
	mockClient := new(exoscaleClientMock)
	cache := newExoscaleCache(mockClient, false)
	ts.p.manager.client = cache

	// Setup: Add node groups to manager
	instanceIDs := []string{testInstanceID}
	ts.p.manager.nodeGroups = []cloudprovider.NodeGroup{
		&instancePoolNodeGroup{
			m: ts.p.manager,
			instancePool: &egoscale.InstancePool{
				ID:          &testInstancePoolID,
				Name:        &testInstancePoolName,
				InstanceIDs: &instanceIDs,
			},
		},
	}

	// Mock API calls - each should only be called ONCE in first loop
	poolInstanceIDs := []string{testInstanceID}
	mockClient.On("GetInstancePool", ts.p.manager.ctx, ts.p.manager.zone, testInstancePoolID).
		Return(
			&egoscale.InstancePool{
				ID:          &testInstancePoolID,
				Name:        &testInstancePoolName,
				InstanceIDs: &poolInstanceIDs,
			},
			nil,
		).Once()

	mockClient.On("GetInstance", ts.p.manager.ctx, ts.p.manager.zone, testInstanceID).
		Return(
			&egoscale.Instance{
				ID:    &testInstanceID,
				Name:  &testInstanceName,
				State: &testInstanceState,
			},
			nil,
		).Once()

	// Simulate autoscaler loop 1: Refresh + Nodes
	err := ts.p.manager.Refresh()
	ts.Require().NoError(err)

	ng := ts.p.manager.nodeGroups[0].(*instancePoolNodeGroup)
	nodes1, err := ng.Nodes()
	ts.Require().NoError(err)
	ts.Require().Len(nodes1, 1)

	// Simulate autoscaler loop 2: Refresh + Nodes (within TTL - cache hits)
	err = ts.p.manager.Refresh()
	ts.Require().NoError(err)

	nodes2, err := ng.Nodes()
	ts.Require().NoError(err)
	ts.Require().Len(nodes2, 1)

	// Simulate autoscaler loop 3: Refresh + Nodes (within TTL - cache hits)
	err = ts.p.manager.Refresh()
	ts.Require().NoError(err)

	nodes3, err := ng.Nodes()
	ts.Require().NoError(err)
	ts.Require().Len(nodes3, 1)

	// Verify API calls only happened once (first loop)
	// Subsequent loops used cache exclusively
	mockClient.AssertExpectations(ts.T())
}

func TestSuiteExoscaleCloudProvider(t *testing.T) {
	suite.Run(t, new(cloudProviderTestSuite))
}
