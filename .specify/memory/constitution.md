<!--
  SYNC IMPACT REPORT
  ==================
  Version Change: None → 1.0.0 (Initial ratification)

  Modified Principles:
  - None (initial creation)

  Added Sections:
  - Core Principles (I-V)
  - Development Constraints
  - Testing & Quality Standards
  - Governance

  Removed Sections:
  - None (initial creation)

  Templates Status:
  ✅ plan-template.md - Reviewed, aligned with constitution check requirements
  ✅ spec-template.md - Reviewed, aligned with scope/requirements constraints
  ✅ tasks-template.md - Reviewed, aligned with testing discipline principle
  ✅ Command files - Reviewed, no agent-specific references found

  Follow-up TODOs:
  - None
-->

# Kubernetes Cluster Autoscaler Exoscale Provider Constitution

## Core Principles

### I. Minimal Architectural Changes

The Exoscale cloud provider development MUST NOT introduce major changes to the cluster-autoscaler architecture or core structure. All changes MUST be contained within the Exoscale provider boundary.

**Rationale**: This ensures compatibility with upstream Kubernetes autoscaler project, simplifies maintenance, reduces risk of breaking changes, and maintains consistency with the broader cluster-autoscaler ecosystem.

**Non-negotiable rules**:
- Architecture and core structure remain unchanged
- Changes are strictly isolated to `./cluster-autoscaler/cloudprovider/exoscale/` directory
- No modifications to shared cluster-autoscaler core code outside the Exoscale provider
- No changes to the cluster-autoscaler API contracts unless absolutely necessary and approved by upstream maintainers

### II. Cross-Provider Pattern Alignment

For new features, implementations MUST follow patterns established by other cloud providers when reasonable. Developers MUST research how other providers in `./cluster-autoscaler/cloudprovider/` implemented similar features before designing new solutions.

**Rationale**: Consistency across providers reduces cognitive load for maintainers, improves code quality through proven patterns, and increases likelihood of upstream acceptance. The cluster-autoscaler is a multi-provider project where consistency matters.

**Non-negotiable rules**:
- Survey at least 3 comparable cloud providers before implementing new features
- Document which providers were reviewed and why their approach was or wasn't adopted
- Justify any significant deviations from established patterns
- Follow naming conventions, code organization, and interface patterns from existing providers

### III. Dependency Reuse (NON-NEGOTIABLE)

When new dependencies are required, developers MUST first examine dependencies used by other cloud providers. Reuse existing dependencies if they satisfy the requirement. Introducing new dependencies requires explicit justification documenting why existing options are insufficient.

**Rationale**: Minimizing unique dependencies reduces binary size, simplifies security audits, decreases supply chain risk, and maintains compatibility with the cluster-autoscaler build system.

**Non-negotiable rules**:
- Survey `./cluster-autoscaler/cloudprovider/*/go.mod` files before adding dependencies
- Reuse shared dependencies from other providers when functionally equivalent
- Document dependency decisions in commit messages or design documents
- New dependencies MUST be justified with: (a) survey of existing options, (b) why they don't meet needs, (c) security/maintenance assessment of proposed dependency

### IV. Comprehensive Testing

Reasonable tests MUST be written for each code change and every new function. "Reasonable" means tests that verify the correctness of the implementation, handle edge cases, and prevent regressions.

**Rationale**: The cluster-autoscaler directly impacts production workloads. Untested code creates risk of cluster instability, incorrect scaling decisions, and service disruptions. Testing is non-negotiable for production-grade Kubernetes components.

**Non-negotiable rules**:
- Every new function MUST have unit tests
- Every code change MUST include or update relevant tests
- Tests MUST cover happy path, error cases, and edge conditions
- Integration tests MUST be included for API interactions and provider-specific scaling logic
- Test coverage MUST NOT decrease with new changes
- All tests MUST pass before code review approval

### V. Provider Isolation

All code changes MUST be contained within the `./cluster-autoscaler/cloudprovider/exoscale/` directory. No changes to code outside this directory are permitted unless explicitly required for provider integration and approved by upstream maintainers.

**Rationale**: Strict isolation prevents unintended impacts on other providers, simplifies code review, ensures changes can be independently tested, and protects the stability of the broader cluster-autoscaler codebase.

**Non-negotiable rules**:
- Code changes are strictly limited to `./cluster-autoscaler/cloudprovider/exoscale/`
- Any necessary changes outside this directory require upstream maintainer approval
- Provider-specific configuration, constants, and utilities remain within provider boundary
- External changes must be proposed through proper channels and justified

## Development Constraints

### Scope Boundaries

- **In Scope**: Exoscale cloud provider implementation within `./cluster-autoscaler/cloudprovider/exoscale/`
- **In Scope**: Tests for Exoscale provider functionality
- **In Scope**: Documentation specific to Exoscale provider usage
- **Out of Scope**: Modifications to cluster-autoscaler core logic
- **Out of Scope**: Changes to other cloud provider implementations
- **Out of Scope**: Architectural changes affecting other providers

### Code Review Requirements

All changes MUST:
- Include comprehensive tests
- Document which providers were studied for pattern alignment (if applicable)
- Justify any new dependencies
- Maintain backward compatibility unless breaking changes are explicitly approved
- Follow Go coding standards and cluster-autoscaler conventions

### Documentation Standards

- Provider-specific documentation in `./cluster-autoscaler/cloudprovider/exoscale/README.md`
- Inline code comments for complex logic
- Commit messages following conventional commits format
- Design documents for significant features (optional but encouraged)

## Testing & Quality Standards

### Test Categories

1. **Unit Tests**: Test individual functions and components in isolation
2. **Integration Tests**: Test interactions with Exoscale API and cluster-autoscaler core
3. **Contract Tests**: Verify provider implements required interfaces correctly
4. **Edge Case Tests**: Handle boundary conditions, rate limits, API failures

### Quality Gates

Before merging, code MUST:
- Pass all existing tests
- Include new tests for changed functionality
- Maintain or improve test coverage
- Pass linting and formatting checks
- Receive approval from at least one maintainer

### Performance Considerations

- Minimize API calls to Exoscale services
- Implement appropriate caching where reasonable
- Follow established patterns for rate limiting and backoff
- Document performance characteristics of new features

## Governance

### Constitutional Authority

This constitution supersedes informal practices and establishes mandatory standards for Exoscale provider development. All pull requests, code reviews, and design decisions MUST comply with these principles.

### Amendment Procedure

1. Proposed amendments MUST be documented in writing
2. Amendments MUST be reviewed and approved by project maintainers
3. Amendments MUST include migration plan if affecting existing code
4. Version number MUST be updated following semantic versioning

### Versioning Policy

- **MAJOR**: Backward incompatible governance changes, principle removals, or fundamental policy shifts
- **MINOR**: New principles added, materially expanded guidance, new sections
- **PATCH**: Clarifications, wording improvements, typo fixes, non-semantic refinements

### Compliance Review

All PRs MUST verify compliance through:
- Self-review checklist against principles
- Maintainer review for constitutional compliance
- Automated checks where feasible (testing requirements, scope boundaries)

### Enforcement

- Non-compliant PRs MUST be revised before merge
- Complexity violations MUST be explicitly justified and documented
- Repeated violations may result in PR rejection

---

**Version**: 1.0.0 | **Ratified**: 2025-11-17 | **Last Amended**: 2025-11-17
