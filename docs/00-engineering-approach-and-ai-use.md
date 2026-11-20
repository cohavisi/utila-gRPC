# Engineering Overview: Approach and AI Use

**Initial deployment and live verification: 2026-11-19.**
This document explains the engineering decisions, contributor responsibilities,
AI assistance, and evidence behind the implementation. It was written after
deployment, not as a contemporaneous decision log. Later verification and
delivery improvements are identified separately below.

## Roles and responsibilities

I supplied the project requirements, requested explanations and comparisons,
approved the implementation plan, selected the deployment environment, and
authorized billable provisioning and publication. As the scope evolved, I chose
to retain Kustomize and remove Helm support. I also requested a repeat evidence
run with screenshots and, subsequently, pre-commit checks and CI security
scanning.

GitHub Copilot CLI assisted with research, candidate designs, substantial code
and documentation generation, command execution, debugging, and verification.
Verification was executed through Copilot-invoked tools and CI; independent
human re-execution is not claimed. The technical investigations below describe
that assisted workflow.

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
The **initial deployment observation** includes 23 startup failures, followed by
4,821 successful calls and no further failures in that captured observation.
It is not a guarantee of lossless failover.

## Later live verification: 2026-11-19

A [separate live evidence run](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/part-2-tasks-3-6-live-evidence-20261119T152720Z.md)
captured **5,259 successful RPCs, zero recorded failures, and no missing or
duplicate call numbers** within its continuous capture. Five actual Argo CD
screenshots accompany baseline, scale-out, graceful pod replacement, server
rollout, and restoration observations.

The original client remained unchanged with zero restarts. The final captured
state had three Ready servers and exactly three healthy backend endpoints,
without retired unhealthy entries. This run is separate from the initial
deployment observation; their totals must not be combined.

## Subsequent delivery and security improvements

The subsequent delivery improvements added:

- Versioned pre-commit hooks in both repositories. Checks run against isolated
  staged snapshots, preserving unstaged edits and the deployed checkout's
  Terraform backend configuration.
- Gitleaks Git-history scans in both CI workflows; Go binary-symbol analysis
  with `govulncheck` and Trivy container scanning in the application workflow.
- Ruff lint/format checks and Trivy analysis of Terraform, rendered Kustomize
  workloads, and local Argo CD manifests in the infrastructure workflow.
- Downloadable Go and Python coverage reports, without a minimum threshold.
  These expose untested paths rather than establishing comprehensive coverage.

The new container scan identified two high-severity dependency advisories.
The affected dependencies were updated to fixed versions rather than adding
exceptions. Both updated workflows passed:
[application](https://github.com/cohavisi/utila-gRPC/actions/runs/44691553606)
and [infrastructure](https://github.com/cohavisi/utila-gRPC-Infrastructure/actions/runs/44691553228).
These changes did not deploy a new application image or modify GCP resources.

## Risk controls and limits

Cloud actions were scoped to the approved environment; local CLI configuration
was isolated. Credentials, real `.tfvars`, and Terraform state were excluded
from Git. Images were digest-pinned, and replica/rollout changes went through
GitOps rather than imperative scaling. Terraform, Argo CD, and the NEG
controller retained distinct ownership; cleanup order was documented.

The demonstration does not establish production readiness, application mTLS,
node/network-failure resilience, or long-lived-stream behavior. Time savings
and performance improvements were not benchmarked. `WaitForReady` can absorb
transient unavailability within an RPC deadline; zero recorded failures does
not establish zero added latency. Security scans cover known findings and
configured policies, not every possible vulnerability. The
[decision-change criteria](part-2-tasks-3-6-integration-decision.md#when-this-decision-should-change)
explain how additional requirements would change the architecture.

For the full sequence, read the [research comparison](part-1-task-2-solution-comparison.md),
[delivery plan](part-2-tasks-3-6-implementation-plan.md), [integration decision](part-2-tasks-3-6-integration-decision.md),
and linked runtime evidence.
