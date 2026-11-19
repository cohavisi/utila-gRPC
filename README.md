# Proxyless Go gRPC on Cloud Service Mesh

Application repository for the DevOps assignment. Infrastructure, Kubernetes
configuration, and Argo CD live in
[utila-gRPC-Infrastructure](https://github.com/cohavisi/utila-gRPC-Infrastructure).

**Status:** application and local xDS integration are implemented. Cloud
provisioning and deployment are in progress; managed xDS verification and
screenshots remain outstanding. Local tests are not evidence of a working
Google-managed control-plane connection.

## Assignment guide

| Requirement | Implementation / explanation |
| --- | --- |
| Part 1, Task 1 | [gRPC load-balancing analysis](docs/task-1-grpc-load-balancing.md) |
| Part 1, Task 2 | [Solution comparison](docs/task-2-solution-comparison.md) |
| Part 2 architecture | [Integration decision](docs/part-2-integration-decision.md) and [implementation plan](docs/part-2-implementation-plan.md) |
| Task 3: GCP IaC | Companion repository, `terraform/` |
| Task 4: server | `cmd/server`, `internal/greeter`, `api/greeter/v1` |
| Task 5: client | `cmd/client` |
| Task 6: GitOps | Companion repository, `gitops/` |

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
