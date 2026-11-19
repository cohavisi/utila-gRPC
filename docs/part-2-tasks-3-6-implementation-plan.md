# Part 2, Tasks 3-6: Proxyless gRPC Implementation Plan

## Objective

Deploy a working Go gRPC client and server on GKE using Google Cloud Service
Mesh's proxyless architecture, provision infrastructure through IaC, and deploy
Kubernetes resources through GitOps.

This implementation adopts Cloud Service Mesh to demonstrate managed discovery
and policy beyond the minimal balancing recommendation in
[Task 2](part-1-task-2-solution-comparison.md).

This records the implementation plan; the deployment and its demonstration
are now complete. The numbered steps describe the delivery sequence; the current
milestone and linked evidence record its outcome. The separately written
[AI-use retrospective](00-engineering-approach-and-ai-use.md) explains roles and
decisions without presenting them as contemporaneous notes.
Application code, containers, Terraform stacks, and Argo CD/Kustomize manifests
are published in the two repositories. Follow the
[application README](../README.md) and
[infrastructure runbook](https://github.com/cohavisi/utila-gRPC-Infrastructure#readme).
The [deployment evidence](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md)
records managed xDS verification, experiments, screenshots, and limitations.

## 1. Confirm the Cloud Service Mesh integration

Before provisioning the full environment, select and verify one supported GKE
proxyless configuration path.

Check:

- Required GKE configuration, APIs, permissions, and fleet registration.
- Go gRPC compatibility and bootstrap requirements.
- How Kubernetes endpoints become available to the control plane.
- Terraform support for the required resources.
- Whether the path supplies the server-side xDS configuration needed by Task 4.

Importing the xDS package does not prove that the server uses xDS. Client
discovery/balancing and server-side xDS configuration are separate
responsibilities. Do not mix Kubernetes Gateway API resources with similarly
named Google Cloud service-routing resources.

**Outcome:** recorded in the
[integration decision](part-2-tasks-3-6-integration-decision.md). The selected path uses
Google Cloud service-routing APIs, GKE standalone NEGs, and a `GRPC_SERVER`
endpoint policy, rather than GKE Gateway API onboarding. Native Terraform schema
coverage and Go/bootstrap requirements are confirmed. The selected integration
has also been verified against the live managed control plane.

## 2. Establish the two repositories

Separate application and infrastructure/GitOps repositories keep application
delivery and infrastructure configuration distinct.

| Repository | Contents |
| --- | --- |
| Application: `utila-gRPC` | Existing analysis, protobuf definition, Go server/client, Dockerfile, build instructions, application CI |
| Infrastructure/GitOps | Terraform, Kubernetes manifests, Argo CD configuration, deployment instructions |

Use immutable image tags or digests so a Git revision identifies exactly what
is deployed.

## 3. Implement and containerize the Go application

### Server: Task 4

- Define and implement `SayHello`.
- Listen on port **50051**.
- Implement the server-side xDS integration verified in step 1.
- Add gRPC health checking and graceful shutdown.
- Include the serving pod identity in responses or logs to demonstrate
  distribution.

### Client: Task 5

- Register xDS support and consume bootstrap configuration.
- Connect through an `xds:///...` target matching the configured service.
- Call `SayHello` with a deadline.
- Support repeated calls through a reused channel, with configurable count/rate.
- Report failures explicitly.

Add focused tests for application behavior and configuration failures. Build
reproducible container images using a multi-stage Dockerfile.

**Outcome:** application code and images ready for the cloud deployment.

## 4. Provision GCP infrastructure with Terraform: Task 3

Create the resources required by the chosen integration:

- Required project APIs.
- VPC/subnet configuration and narrowly scoped firewall rules.
- A small GKE cluster suitable for the demonstration.
- Artifact Registry.
- Workload identities and least-privilege permissions.
- Cloud Service Mesh configuration and any required fleet resources.
- Endpoint registration, routing, and health-check resources appropriate to
  that model.

Configure Terraform state securely and document teardown. Avoid credentials in
Git, and account for ongoing cluster and mesh charges.

**Outcome:** reproducible infrastructure without undocumented console changes.

## 5. Deploy through Argo CD: Task 6

Bootstrap Argo CD and point it at the GitOps repository.

Have Argo CD reconcile:

- Application namespace, service accounts, and configuration.
- Server Deployment and Service.
- Client workload.
- Bootstrap mounts or generation configuration.
- Readiness checks and resource requests/limits.

Start with three server replicas so distribution is visible.

**Ownership boundary:** Terraform provisions cloud infrastructure; Argo CD
reconciles Kubernetes resources. Extending Argo CD management to Terraform
execution requires an explicit additional integration; Argo CD does not run
Terraform natively. The selected model has no Kubernetes mesh-routing CRDs:
Terraform owns the Google Cloud `Mesh`, `GRPCRoute`, and server endpoint policy. Avoid two
systems managing the same resource.

**Outcome:** application deployments and Kubernetes configuration reconciled
from Git, with the infrastructure provisioning boundary documented.

## 6. Demonstrate the actual behavior

| Scenario | Evidence |
| --- | --- |
| Successful communication | Client output showing `SayHello` responses |
| RPC distribution | Repeated calls reaching multiple server pods through one reused channel |
| Scale-out | New replicas receiving calls without restarting the client |
| Backend failure | Observed errors and recovery after a pod failure |
| Graceful rollout | Calls and draining behavior during a server update |
| Genuine proxyless operation | Accepted xDS configuration and no traffic-proxy sidecars |
| GitOps operation | Argo CD showing the deployed Git revision as synchronized and healthy |

Do not require perfectly equal call counts: policy, locality, timing, and health
can affect distribution. Existing streaming RPCs cannot be transparently moved
to another backend.

Verify any implemented application transport security and telemetry explicitly;
access to the xDS control plane alone does not establish application mTLS.

**Outcome:** recorded observations supporting the deployment's architectural
claims, not just screenshots of running pods.

## 7. Package the project documentation

Provide concise setup, architecture, verification, limitations, and cleanup
instructions.

Capture screenshots of:

- Argo CD synchronization and application health.
- Kubernetes workloads.
- Mesh/backend health.
- Successful calls distributed across server replicas.

Clearly distinguish implemented and demonstrated behavior from production
improvements such as broader availability, advanced security policies, and
additional resilience testing.

**Outcome:** both repositories, a technical write-up, and deployment evidence
that a reviewer can follow and reproduce.

## Current milestone

The integration model and server-side xDS mechanism are documented in the
[integration decision](part-2-tasks-3-6-integration-decision.md). Both repositories now
contain their published implementation. The image is published and digest-pinned;
GKE, the managed mesh, and Argo CD are running in `vast-maxim-388708`,
`us-central1-a`. Managed xDS, three-to-five-backend distribution, pod replacement,
and server rollout were observed without restarting the client. Three server
replicas are restored. Screenshots, measured windows, initial discovery errors,
and scope limitations are linked above. Resources remain running and billable;
use the runbook's ordered teardown only when the review environment is no
longer required.
