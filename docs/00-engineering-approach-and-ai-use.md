# Engineering Overview: Approach and AI Use

**Retrospective, 2026-11-19.** This account was written after deployment. It
summarizes the recorded workflow, not a contemporaneous decision log or a claim
that the final design was known from the start.

## Roles and responsibilities

The human contributor supplied the assignment, requested explanations and
comparisons, approved the implementation plan, selected the deployment
environment, and explicitly authorized billable provisioning and publication.

GitHub Copilot CLI assisted with research, candidate designs, substantial code
and documentation generation, command execution, debugging, and verification.
The decisions below emerged from that assisted workflow; they are not presented
as unaided human discoveries. Cited verification was executed through
AI-invoked tools and CI, not necessarily through manually repeated commands or
an independent human review.

## Working method

The workflow progressed from problem analysis to alternatives, an implementation
plan, integration checks, deployment, and observed behavior. AI-generated
proposals were evaluated against official documentation, pinned provider/library
capabilities, and executable evidence rather than accepted on plausibility.

Acceptance required actual server-side xDS, successful calls through managed
discovery, and endpoint changes on the same client. A valid Terraform resource,
a listening socket, or an Argo CD green status was not sufficient by itself.
The local ADS fixture and mock-provider checks were kept distinct from live
Google-managed xDS verification.

## Decision story 1: a documented path was not enough

**Question:** could GKE Gateway API onboarding be expressed through the selected
Terraform provider without an imperative workaround?

AI-assisted research identified the candidate. Provider-schema inspection and
the pinned beta provider source did not expose its required `config_api`
setting. That finding was scoped to the checked versions, not generalized into
a claim that Gateway API or Terraform integration was impossible.

**Decision:** use native service-routing resources and standalone NEGs, avoiding
`local-exec` and supporting the required server endpoint policy.

**Evidence:** the [recorded rationale and primary references](part-2-tasks-3-6-integration-decision.md#why-this-path)
are included in [application source commit `97c8a93`](https://github.com/cohavisi/utila-gRPC/commit/97c8a93b730129e1870f824d20b3dbf998a6346e).
The selected path was subsequently exercised in the live deployment.

## Decision story 2: green resources, failing RPCs

**Question:** why did the client lack a usable Listener when the API accepted
the route and server/backend health was green?

The initial generated route referenced a relative backend `.id`. Tool-driven
investigation led to the canonical `.self_link` reference. The correction
restored calls without restarting the client, separating a routing problem
from an application restart workaround.

**Decision:** require end-to-end RPC evidence, not just provisioning/readiness
signals. Keep the failed startup observations in the report.

**Evidence:** [route fix `fc4d032`](https://github.com/cohavisi/utila-gRPC-Infrastructure/commit/fc4d032)
and the [runtime evidence](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md).
The captured lifetime snapshot includes 23 initial failures, then 4,821 successes
and zero later failures; it is not a guarantee of lossless failover.

## Risk controls and limits

Cloud actions were scoped to the approved environment; local CLI configuration
was isolated. Credentials, real `.tfvars`, and Terraform state were excluded
from Git. Images were digest-pinned, and replica/rollout changes went through
GitOps rather than imperative scaling. Terraform, Argo CD, and the NEG
controller retained distinct ownership; cleanup order was documented.

The demonstration does not establish production readiness, application mTLS,
node/network-failure resilience, or long-lived-stream behavior. Time savings
and performance improvements were not benchmarked. The
[decision-change criteria](part-2-tasks-3-6-integration-decision.md#when-this-decision-should-change)
explain how additional requirements would change the architecture.

For the full sequence, read the [research comparison](part-1-task-2-solution-comparison.md),
[delivery plan](part-2-tasks-3-6-implementation-plan.md), [integration decision](part-2-tasks-3-6-integration-decision.md),
and linked runtime evidence.
