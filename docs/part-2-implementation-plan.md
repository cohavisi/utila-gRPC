# Part 2: Proxyless gRPC Implementation Plan

## Objective

Deploy a working Go gRPC client and server on GKE using Google Cloud Service
Mesh's proxyless architecture, provision infrastructure through IaC, and deploy
Kubernetes resources through GitOps.

Cloud Service Mesh is mandatory for this part regardless of the architectural
recommendation in [Task 2](task-2-solution-comparison.md).

This plan describes the full submission, not a claim that deployment is complete.
Application code, containers, Terraform stacks, and Argo CD/Kustomize manifests
are implemented locally in the two repositories. Follow the
[application README](../README.md) and
[infrastructure runbook](https://github.com/cohavisi/utila-gRPC-Infrastructure#readme).
Cloud provisioning is in progress. Managed xDS verification, deployment
experiments, and screenshots remain outstanding.

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
[integration decision](part-2-integration-decision.md). The selected path uses
Google Cloud service-routing APIs, GKE standalone NEGs, and a `GRPC_SERVER`
endpoint policy, rather than GKE Gateway API onboarding. Native Terraform schema
coverage and Go/bootstrap requirements are confirmed; live GCP verification
remains a deployment gate.

## 2. Establish the two repositories

The assignment requests separate application and infrastructure/GitOps
repositories.

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
- Supported Kubernetes mesh routing resources.
- Readiness checks and resource requests/limits.

Start with three server replicas so distribution is visible.

**Ownership boundary:** Terraform provisions cloud infrastructure; Argo CD
reconciles Kubernetes resources. If the assignment's "fully managed by ArgoCD"
wording is interpreted to include Terraform execution, that requires an
explicit additional integration, not an assumption that Argo CD runs Terraform
natively. Avoid two systems managing the same resource.

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

## 7. Package the submission

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
[integration decision](part-2-integration-decision.md). Both repositories now
contain their implementation. Image/toolchain availability and local
implementation checks are now complete. The GCP project/location are selected
and billable provisioning is approved. Complete provisioning, publish the image,
and follow the staged deployment runbook to satisfy the remaining managed xDS
and evidence gates.
