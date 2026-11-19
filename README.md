# Proxyless Go gRPC on Cloud Service Mesh

Application repository for the DevOps assignment. Infrastructure, Kubernetes
configuration, and Argo CD live in
[utila-gRPC-Infrastructure](https://github.com/cohavisi/utila-gRPC-Infrastructure).

**Status:** deployed and verified against Google-managed Cloud Service Mesh in
`vast-maxim-388708` (`us-central1-a`). Argo CD is Healthy/Synced; the same running
client demonstrated distribution, GitOps scale-out, pod replacement, and server
rollout. See [deployment evidence and screenshots](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md),
including the initial discovery errors and measured observation windows.

## Assignment guide

Document names identify the assignment part and task. The Part 2 plan and
integration decision cover Tasks 3-6 together; the `00-` overview spans the
assignment rather than representing an additional task.

| Requirement | Implementation / explanation |
| --- | --- |
| Engineering approach | [AI-assisted workflow, decision stories, and verification boundaries](docs/00-engineering-approach-and-ai-use.md) |
| Part 1, Task 1 | [gRPC load-balancing analysis](docs/part-1-task-1-grpc-load-balancing.md) |
| Part 1, Task 2 | [Solution comparison](docs/part-1-task-2-solution-comparison.md) |
| Part 2 architecture | [Integration decision](docs/part-2-tasks-3-6-integration-decision.md) and [implementation plan](docs/part-2-tasks-3-6-implementation-plan.md) |
| Task 3: GCP IaC | Companion repository, [Terraform](https://github.com/cohavisi/utila-gRPC-Infrastructure/tree/main/terraform) |
| Task 4: server | [`server.go`](cmd/server/server.go), [`SayHello`](internal/greeter/service.go), [protobuf contract](api/greeter/v1/greeter.proto) |
| Task 5: client | [`client.go`](cmd/client/client.go) |
| Task 6: GitOps | Companion repository, [Argo CD Application](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/gitops/argocd/application.yaml) and [manifests](https://github.com/cohavisi/utila-gRPC-Infrastructure/tree/main/gitops) |
| Optional Helm version | Companion repository, [chart](https://github.com/cohavisi/utila-gRPC-Infrastructure/tree/main/gitops/helm/greeter) and [migration guide](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/part-2-tasks-3-6-kustomize-to-helm.md); live demo remains on Kustomize |
| Live deployment | Companion repository, [evidence](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md) |

Task 3 uses Terraform to provision GCP; Task 6 uses Argo CD to reconcile the
application's Kubernetes manifests. Argo CD does not execute Terraform. This is
the documented interpretation of "fully managed by ArgoCD" for the Kubernetes
deployment, not a claim that cloud provisioning is Argo-managed.

## Design

`SayHello` returns a greeting and the serving pod's hostname (`server_id`).
The server uses **`xds.NewGRPCServer`**, listens on `50051`, and consumes inbound
Listener configuration. The client uses **`xds:///greeter.utila.internal:50051`**
and reuses one channel, allowing the managed xDS policy to distribute new RPCs.
There is no traffic-proxy sidecar or production-local control plane.

The explicit demo baseline is **plaintext application traffic on private GKE
pod addresses**. Google control-plane access remains authenticated and encrypted.
This is not application mTLS or a production security claim.

Port `50052` is internal health/admin only. Default gRPC health reports serving
only after accepted xDS configuration; the separate `liveness` health service
does not depend on xDS availability. Shutdown marks health non-serving and
allows up to 20 seconds to drain before forcing termination.

## Local development

Use Go **1.27.4**; `go.mod` selects that toolchain. An older Go installation
with automatic toolchain downloads enabled can download it.

```sh
make test build
```

The integration test starts an in-process ADS test fixture and actual xDS gRPC
servers. It verifies refusal to serve before Listener configuration, balancing
across two discovered servers, and adoption of a third EDS endpoint through the
same client channel. It does **not** substitute for live Cloud Service Mesh tests.

Generated protobuf sources are included. Regenerate with `protoc` **36.2**
installed, using `make proto`; the Makefile pins both Go generator versions.
CI verifies regeneration, formatting, race tests, vet, builds, and the container.

## Build and run

```sh
docker build --platform linux/amd64 -t utila-grpc:local .
docker run --rm utila-grpc:local -help
docker run --rm --entrypoint /client utila-grpc:local -help
```

One nonroot, digest-pinned image contains `/server` and `/client`. Linux amd64
matches the pinned bootstrap image and GKE node selector. The Dockerfile also
supports cross-compilation through BuildKit target arguments.

The deployed image was built from application source commit
`97c8a93b730129e1870f824d20b3dbf998a6346e`; later documentation-only commits do
not change that image's source identity. The infrastructure overlay pins its
Artifact Registry digest.

Both binaries require `GRPC_XDS_BOOTSTRAP` pointing to a valid bootstrap file.
They deliberately do not fall back to DNS or an ordinary non-xDS server.
The deployment generates that file in an init container, not a traffic sidecar.
Use the companion repository's deployment procedure for real bootstrap/IAM.

The client supports `-count`, `-interval`, `-timeout`, `-name`, and `-target`.
`-count=0` runs until interrupted. Each call has a deadline, failures are logged,
and finite runs exit nonzero if any call fails. JSON success logs contain
`server_id`; shutdown also emits counts per backend.

For reviewer deployment and evidence commands, follow the
[infrastructure runbook](https://github.com/cohavisi/utila-gRPC-Infrastructure#readme).
