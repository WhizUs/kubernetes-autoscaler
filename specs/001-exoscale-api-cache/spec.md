# Feature Specification: Exoscale API Caching Layer

**Feature Branch**: `001-exoscale-api-cache`
**Created**: 2025-11-17
**Status**: Draft
**Input**: User description: "Implement caching for all calls to the Exoscale API, in order to increase performance. Cache instances for 5 minutes. Cache quota for 1 hour.  Cache SKS clusters for 10 minutes. Cache instance pools for 5 minutes. Implement Jitter for the api calls. Once all is implemented, consecutive function loops should not need any further api calls, they should only need to read the cached objects."

## Clarifications

### Session 2025-11-17

- Q: When an API call fails during cache refresh, should the system keep serving stale (expired) cached data or return an error? → A: Serve stale cached data with extended TTL until API recovers (graceful degradation)
- Q: What should the jitter window duration be for spreading out API requests? → A: Proportional jitter (10-30% of cache TTL)
- Q: Should the cache be scoped to a single autoscaler process (in-memory per-process) or shared across multiple autoscaler instances? → A: Process-local in-memory cache (each autoscaler instance maintains its own cache)
- Q: When the autoscaler starts with an empty cache, how should it handle the initial data fetching? → A: Fetch all required data synchronously on first access (blocking until data available)
- Q: At what granularity should cache hit/miss metrics be tracked? → A: Per-resource-type metrics (separate hit rates for instances, pools, clusters, quota)

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Reduced API Call Latency (Priority: P1)

As a cluster operator running Cluster Autoscaler with the Exoscale provider, I need the autoscaler to respond faster to scaling decisions by reading cached data instead of making repeated API calls to Exoscale, so that my cluster can scale up or down more quickly when workload demands change.

**Why this priority**: This is the core value proposition - reducing latency directly improves cluster responsiveness and reduces time-to-scale for applications.

**Independent Test**: Can be fully tested by monitoring API call frequency and measuring time-to-decision in autoscaler loops. Delivers immediate value by speeding up scaling operations.

**Acceptance Scenarios**:

1. **Given** the autoscaler has fetched instance pool data within the last 5 minutes, **When** a scaling decision loop runs, **Then** the autoscaler uses cached instance pool data without making a new API call
2. **Given** the autoscaler has fetched SKS cluster data within the last 10 minutes, **When** node group information is requested, **Then** the autoscaler returns cached cluster data without making a new API call
3. **Given** the autoscaler has fetched instance data within the last 5 minutes, **When** node status checks are performed, **Then** the autoscaler uses cached instance data
4. **Given** cached data exists and is still valid, **When** consecutive autoscaler loops execute within the cache validity period, **Then** zero API calls are made to Exoscale

---

### User Story 2 - Reduced Exoscale API Load (Priority: P2)

As a cluster operator, I need the autoscaler to minimize the number of API requests to Exoscale, so that I avoid hitting API rate limits and reduce the load on Exoscale's infrastructure.

**Why this priority**: Prevents operational issues caused by rate limiting while being a good API citizen. Critical for stability but secondary to performance improvement.

**Independent Test**: Can be tested independently by monitoring Exoscale API request counts over time and verifying they decrease significantly compared to non-cached baseline.

**Acceptance Scenarios**:

1. **Given** the autoscaler runs scaling loops every 10 seconds, **When** operating under normal conditions, **Then** API calls to fetch instances occur at most once every 5 minutes per autoscaler process (not every 10 seconds)
2. **Given** the autoscaler runs scaling loops every 10 seconds, **When** operating under normal conditions, **Then** API calls to fetch instance pools occur at most once every 5 minutes per autoscaler process
3. **Given** the autoscaler runs scaling loops every 10 seconds, **When** operating under normal conditions, **Then** API calls to fetch SKS clusters occur at most once every 10 minutes per autoscaler process
4. **Given** the autoscaler runs scaling loops every 10 seconds, **When** operating under normal conditions, **Then** API calls to fetch quota occur at most once every hour per autoscaler process
5. **Given** a single autoscaler process runs multiple concurrent scaling loops, **When** the same resource type is queried, **Then** the process-local cache eliminates redundant API calls within that process

---

### User Story 3 - Resilient API Request Distribution (Priority: P3)

As a cluster operator, I need API requests to be distributed with jitter when cache expires, so that multiple autoscaler instances or tight loops don't cause synchronized API request storms that could overwhelm the Exoscale API or trigger rate limiting.

**Why this priority**: Prevents edge case issues with synchronized cache expiration. Important for large-scale deployments but not critical for basic functionality.

**Independent Test**: Can be tested by running multiple autoscaler instances and measuring API request timing distribution when caches expire simultaneously.

**Acceptance Scenarios**:

1. **Given** cache entries for the same resource type expire at approximately the same time, **When** multiple components attempt to refresh the cache, **Then** requests are spread over a random jitter window to prevent simultaneous API calls
2. **Given** cached data has expired, **When** the autoscaler needs to refresh the cache, **Then** a random jitter delay is applied before making the API call
3. **Given** multiple autoscaler loops run concurrently, **When** cache refresh is needed, **Then** request timing varies randomly to distribute load

---

### Edge Cases

- What happens when cached data expires mid-operation during a scaling decision?
- How does the system handle cache corruption or invalid cached data?
- When an API call fails during cache refresh, the system serves stale cached data with extended TTL until API recovers (graceful degradation)
- When the autoscaler first starts with an empty cache, it fetches all required data synchronously on first access, blocking until data is available to ensure reliable scaling decisions
- What happens if system time changes or clock skew occurs, affecting cache expiration calculations?
- How does the cache handle concurrent access from multiple goroutines within a process?
- What occurs when Exoscale API returns updated data that conflicts with cached state?
- How are cache entries invalidated when administrative actions modify resources outside the autoscaler?

## Requirements *(mandatory)*

### Functional Requirements

**Core Caching**:
- **FR-001**: System MUST cache Exoscale compute instance data for 5 minutes after retrieval
- **FR-002**: System MUST cache Exoscale quota information for 1 hour after retrieval
- **FR-003**: System MUST cache SKS cluster data for 10 minutes after retrieval
- **FR-004**: System MUST cache instance pool data for 5 minutes after retrieval
- **FR-005**: System MUST automatically expire and refresh cache entries when their time-to-live (TTL) period elapses
- **FR-006**: System MUST serve data from cache when valid cached data exists, without making API calls to Exoscale
- **FR-007**: System MUST apply random jitter (10-30% of cache TTL) to API request timing to prevent synchronized request storms
- **FR-008**: Consecutive autoscaler evaluation loops MUST be able to complete using only cached data without making additional API calls (when cache is valid)
- **FR-009**: System MUST make an API call to refresh cache when requested data is not cached or cache has expired
- **FR-010**: System MUST handle cache misses gracefully by fetching data synchronously from the API (blocking until data available) and populating the cache

**Reliability & Safety**:
- **FR-011**: System MUST ensure thread-safe or concurrency-safe access to cached data within a single autoscaler process
- **FR-012**: System MUST handle API failures during cache refresh by serving stale cached data with extended TTL until API recovers (graceful degradation), without corrupting cached data
- **FR-013**: System MUST continue serving stale cached data when API refresh fails, extending TTL until successful API response
- **FR-014**: System MUST implement the cache using interface wrapping pattern to allow transparent integration without modifying existing call sites

**Observability & Testing**:
- **FR-015**: System MUST track cache hit and miss rates per resource type (instances, instance pools, SKS clusters, quota) for observability
- **FR-016**: System MUST provide cache invalidation mechanism for debugging and testing purposes
- **FR-017**: System MUST provide runtime configuration to enable/disable TTL jitter for testing determinism

**Integration**:
- **FR-018**: System MUST integrate cache at a single integration point where all API calls flow through (wrapper pattern)
- **FR-019**: System MUST preserve existing test file structure and organization when adding cache-related tests
- **FR-020**: System MUST implement defensive nil-checking in test mocks to prevent panics during test execution

### Key Entities

- **Cached Instance**: Represents compute instance data retrieved from Exoscale API, includes instance metadata, state, and configuration, with 5-minute TTL
- **Cached Quota**: Represents account quota limits and usage from Exoscale API, includes resource limits and current consumption, with 1-hour TTL
- **Cached SKS Cluster**: Represents Kubernetes cluster configuration from Exoscale SKS API, includes node pool information and cluster state, with 10-minute TTL
- **Cached Instance Pool**: Represents instance pool configuration and membership from Exoscale API, includes pool size, instance IDs, and scaling parameters, with 5-minute TTL
- **Cache Entry Metadata**: Tracks timestamp of cache creation, TTL duration, expiration time, and validity status for each cached resource
- **Cache Metrics**: Per-resource-type tracking of cache hits, misses, and hit rates for instances, instance pools, SKS clusters, and quota

## Success Criteria *(mandatory)*

### Measurable Outcomes

**Performance**:
- **SC-001**: Autoscaler scaling decision latency decreases by at least 50% compared to non-cached baseline when cache is warm
- **SC-002**: API calls to Exoscale decrease by at least 90% during normal operation when cache hit rate is high
- **SC-003**: Consecutive autoscaler evaluation loops complete without external API calls 95% of the time (when operating within cache validity windows)
- **SC-004**: Cache hit rate exceeds 85% during steady-state cluster operation
- **SC-005**: API request rate limiting events from Exoscale decrease to near-zero compared to non-cached baseline
- **SC-006**: Time between cluster state changes and scaling actions improves by at least 30%
- **SC-007**: When multiple autoscaler loops run concurrently, API request storms are eliminated (measured by request timestamp distribution showing 10-30% TTL jitter spread)

**Code Quality & Testing**:
- **SC-008**: All tests pass without race conditions when run with race detector (`go test -race`)
- **SC-009**: Test coverage on cache implementation exceeds 80% (measured on cache-specific code paths)
- **SC-010**: Integration tests successfully simulate real autoscaler behavior (90+ tests covering unit, integration, and concurrency scenarios)
- **SC-011**: Implementation requires minimal refactoring after initial design (less than 10% code churn post-implementation)
- **SC-012**: Cache integration requires changes to fewer than 5 non-test files (demonstrating single integration point pattern)

## Assumptions

- The autoscaler runs evaluation loops at regular intervals (typically every 10-30 seconds based on standard cluster-autoscaler configuration)
- Exoscale API data changes infrequently relative to cache TTL periods (instances, pools, and clusters are relatively stable)
- Quota information changes rarely, justifying the 1-hour cache duration
- The autoscaler is the primary consumer of Exoscale API data in this context
- Cache invalidation by external events (manual resource changes) is acceptable with the defined TTL delays
- Memory overhead of caching is negligible compared to operational benefits
- Thread-safe cache implementation is required as cluster-autoscaler is multi-threaded
- Cache persistence across autoscaler restarts is not required (in-memory cache is sufficient)
- Each autoscaler process maintains its own process-local cache (no cross-process cache sharing required)
- Most cluster-autoscaler deployments run a single instance, making process-local cache sufficient

## Constraints

**Code Organization**:
- Cache implementation must remain within the Exoscale cloud provider boundary (`./cluster-autoscaler/cloudprovider/exoscale/`)
- No modifications to cluster-autoscaler core caching mechanisms outside the provider
- Implementation must follow patterns used by other cloud providers where applicable
- Must preserve existing file structure and test organization (mock definitions stay in original test files)

**Configuration**:
- Cache TTL values are fixed as specified (not dynamically adjustable at runtime except jitter toggle for testing)
- Single integration point pattern required (wrap at one chokepoint, not scattered throughout)

**Testing & Validation**:
- All code changes must include comprehensive unit and integration tests
- Tests must validate actual struct field names from source code, not assumptions
- Test mocks must implement defensive nil-checking before type assertions
- Must pass race detector validation (`go test -race`)
- Integration tests must simulate real autoscaler loop behavior (consecutive Refresh() + Nodes() calls)
- Test file structure must be preserved (mocks in original locations, referenced by new tests)

**Dependencies**:
- Implementation must not introduce new dependencies without surveying existing provider dependencies first

---

## Implementation Notes

**Development Approach**:
- Test-first approach with incremental validation recommended
- Run race detector (`go test -race`) throughout development, not just at the end
- Verify actual struct definitions before writing test code

**Common Pitfalls**:
- Don't duplicate mock definitions across test files (keep in original location)
- Always check for nil before type assertions in test mocks
- Verify struct field names match actual definitions (e.g., `m` vs `manager`, `InstanceIDs` vs `Instances`)

**Validation**:
- Run tests after each implementation phase
- Validate 90% API reduction target with integration tests
- Ensure all tests pass with race detector enabled
