# Part 2: Cloud Service Mesh Integration Decision

**Decision date:** 2026-11-19

**Status:** Application, Terraform, and GitOps implementation is available in
the two repositories and locally validated. Live GCP verification remains a
deployment gate. Approved cloud provisioning is now in progress.

## 1. Selected architecture

Use **Cloud Service Mesh with Google Cloud service-routing APIs**, a
**VPC-native GKE Standard cluster**, and **standalone zonal network endpoint
groups (NEGs)**. Run proxyless Go gRPC clients and an xDS-enabled Go server,
without Envoy or Linkerd sidecars.

The cloud configuration consists of:

- A global Network Services `Mesh`.
- A global Network Services `GRPCRoute` mapping
  `greeter.utila.internal:50051` to a Compute backend service.
- A global Compute backend service using `GRPC`,
  `INTERNAL_SELF_MANAGED`, and `ROUND_ROBIN`, referencing GKE-managed NEGs.
- A Network Services endpoint policy of type `GRPC_SERVER`, matching server
  bootstrap metadata and application port `50051`.
- A gRPC health check on a separate internal port, `50052`.

These are Google Cloud resources, **not Kubernetes Gateway API CRDs**.
The logical route name does not need a matching DNS record: clients use
`xds:///greeter.utila.internal:50051`, and the configured route supplies backend
discovery. DNS is still used for ordinary infrastructure connectivity. [1][2]

```text
Terraform --> Mesh + GRPCRoute + backend service + GRPC_SERVER policy
                                      |
                              Cloud Service Mesh
                               /             \
                      client xDS config    server xDS config
                            /                 \
                       Go client ----------> Go server pods :50051
                                  direct RPCs

Argo CD --> Kubernetes Service --> GKE NEG controller --> pod IP:port membership
```

### Why this path

The service-routing model has documented proxyless support and native Terraform
resources for both outbound routing and the inbound server endpoint policy.
GKE standalone NEGs expose pod IP/port endpoints to cloud backend services.
Google's service-security guide explicitly combines GKE, NEGs, `Mesh`,
`GRPCRoute`, and server-side xDS policies. [1][3][4]

The GKE Gateway API path remains a valid documented alternative, but its fleet
onboarding requires `config-api gateway`. The installed Google provider
**8.12.0** exposes only `control_plane` and `management` in the checked
`google_gke_hub_feature_membership.mesh` schema, not `config_api`. The checked
beta provider source also lacks that setting in its mesh schema. This does not
prove that every possible Terraform integration is impossible; it establishes
that the obvious native resource does not express the documented onboarding
step. [5][6]

Rather than add an imperative `local-exec` workaround, use the native
service-routing model. Do not enable fleet mesh `MANAGEMENT_AUTOMATIC`, Gateway
API onboarding, Istio injection, or legacy URL-map/target-gRPC-proxy routing for
this baseline.

Fleet enrollment and CA Service are not part of this non-mTLS baseline. Revisit
them if adopting the documented GKE managed-certificate security integration.

## 2. Client and server xDS responsibilities

### Client

Register `google.golang.org/grpc/xds`, load bootstrap configuration through
`GRPC_XDS_BOOTSTRAP`, and create a channel for
`xds:///greeter.utila.internal:50051`.

The library consumes routing, cluster, and endpoint configuration from Cloud
Service Mesh and selects a backend for each new RPC. It does not send RPCs
through the mesh control plane. Reuse the channel for the balancing
demonstration; do not reconnect for every call. [1][7]

### Server: the important Task 4 distinction

Create the application server with **`xds.NewGRPCServer`**, not merely
`grpc.NewServer` plus a blank xDS import. The Go implementation watches a
server Listener resource using the bootstrap's
`server_listener_resource_name_template`. Without the required bootstrap field,
server construction fails. [8]

The bootstrap generator supplies:

```text
server_listener_resource_name_template:
  grpc/server?xds.resource.listening_address=%s
```

Configure a `GRPC_SERVER` endpoint policy matching bootstrap node metadata
`app=greeter` and traffic port `50051`. Kubernetes labels are not automatically
equivalent to this xDS metadata: the generator must receive the matching
`--node-metadata=app=greeter` argument. [3][9]

This confirms an actual server-side xDS configuration mechanism, not just
client-side discovery. The assignment's phrase "server uses xDS for discovery
and load balancing" is implemented according to the protocol's roles: the
caller discovers and balances; the server consumes inbound listener/policy
configuration.

Log xDS serving-mode changes and make readiness reflect the application's
ability to serve. Keep the health listener separate so it can report
non-serving during initialization and shutdown. A listening TCP socket alone
is not proof of accepted xDS configuration.

### Transport scope

For the assignment's minimal balancing demonstration, the baseline is explicit
**plaintext application gRPC on private pod addresses**, not an assertion of
production security. The endpoint-policy API permits omission of
`serverTlsPolicy`, which disables authentication for that endpoint. Do not
claim mTLS or add a silent plaintext fallback to a purported secure mode. [9]

Control-plane access still uses authenticated, encrypted communication with
Google. Application TLS/mTLS is a separate configuration and certificate
provisioning task; the documented security extension adds significant resources,
including CA Service. If security requirements demand that extension, revise
this decision before deploying application traffic. [3]

## 3. Versions and bootstrap

| Component | Selected baseline | Verification |
| --- | --- | --- |
| Google Terraform provider | `hashicorp/google` **8.12.0** | Installed in an isolated schema-check directory; resource graph validates |
| Go gRPC | **1.85.0** | Released version; its module requires Go **1.26.0** |
| Application build toolchain | Go **1.27.4** | Selected by `go.mod`, CI, and the digest-pinned builder; application and Linux image build locally |
| Bootstrap generator | **0.20.0** | Source, image digest, amd64 architecture, executable flags, and server listener template checked |
| GKE | Standard, VPC-native, supported Regular-channel version | Exact available version and project quota must be checked before creation |

The Dockerfile pins the Go builder and nonroot distroless runtime by digest.
The deployment pins bootstrap image
`sha256:3f8bd252f6e902d1fe8bf834a5f29499dab628920ececb3ebdebd2906f0a641e`
and selects amd64 nodes because that image is single-architecture. The image
was pulled and its executable flags inspected locally; this is not a live
GKE bootstrap or authentication check. Do not copy floating `latest` tags or
old example Go versions into the deployment. [7][10]

Use a bootstrap init container and a shared in-memory volume for each workload:

```text
--config-mesh=utila-grpc
--vpc-network-name=utila-grpc
--node-metadata=app=greeter       # server; use a distinct value for the client
--output=/bootstrap/bootstrap.json
```

The default `Mesh` and VPC are named `utila-grpc`; bootstrap arguments must match
the applied resource names. The configuration script derives them from
Terraform outputs rather than relying on the generator's `default` VPC.
The generator emits the
mesh-specific client listener template, server listener template, node identity,
and xDS v3 configuration. Validate project, locality, and detected pod IP in the
generated output. Do not combine `--config-mesh` with `--generate-mesh-id`;
the generator treats them as mutually exclusive. [10]

## 4. Platform prerequisites and ownership

### GKE configuration

Use one zone initially, with VPC-native pod/service secondary IP ranges,
Workload Identity Federation (`PROJECT_ID.svc.id.goog`), and the GKE metadata
server (`GKE_METADATA`) on the workload node pool. Keep the HTTP load-balancing
add-on enabled for standalone NEG controller support. Do not enable Gateway API
or proxy injection for this integration. Select a currently supported
Regular-channel cluster version rather than copying an obsolete minimum from a
tutorial. [4]

### APIs and access

The infrastructure API set includes `serviceusage.googleapis.com`,
`cloudresourcemanager.googleapis.com`, `compute.googleapis.com`,
`container.googleapis.com`, `networkservices.googleapis.com`,
`trafficdirector.googleapis.com`, `artifactregistry.googleapis.com`, and
`iam.googleapis.com`. Include `sts.googleapis.com` and
`iamcredentials.googleapis.com` for the selected Workload Identity Federation
and Kubernetes-to-Google service-account impersonation setup. Enable diagnostic
APIs only for the logging/monitoring integrations actually configured.

Give the client and server separate Kubernetes/Google service-account bindings.
Their Google service accounts need `roles/trafficdirector.client`, including
`trafficdirector.networks.getConfigs` and
`trafficdirector.networks.reportMetrics`. Scope Workload Identity bindings to the
specific namespace/service-account pairs. Image-pull permissions belong to the
node identity; do not assume pod identity grants node image access. [3][11]

The provisioning identity separately needs permissions to enable APIs, manage
the chosen network and GKE resources, create mesh/backend resources, and manage
the required IAM bindings. Do not grant the runtime applications provisioning
roles or copy tutorial-wide grants to all authenticated workloads.

### Resource boundaries

| Owner | Resources |
| --- | --- |
| Terraform | APIs, network/firewall, GKE/node identities, Artifact Registry, workload IAM bindings, `Mesh`, `GRPCRoute`, endpoint policy, backend service, health check |
| Argo CD | Namespace, Kubernetes service accounts, NEG-annotated Service, Deployments/client workload, bootstrap configuration, probes, resource limits |
| GKE NEG controller | Zonal NEG creation and pod IP/port membership, discovered by Terraform rather than independently recreated |

The Service will expose port `50051` with
`cloud.google.com/neg` standalone-NEG configuration. Use a known NEG name and
discover the actual zones through the Service's NEG status; attach all relevant
zonal NEGs. For the initial single-zone demo, this is one attachment.

Allow private client-to-server traffic on `50051`, DNS and authenticated access
to Google APIs, and health-check traffic on `50052`. Restrict health-check
firewall access to the documented source ranges and target nodes; do not create
a public application LoadBalancer Service.

## 5. Terraform coverage and deployment ordering

An isolated configuration using Google provider **8.12.0** passed
`terraform init -backend=false` and `terraform validate`. It included:

```text
google_network_services_mesh
google_network_services_grpc_route
google_network_services_endpoint_policy (type = GRPC_SERVER)
google_compute_backend_service (GRPC, INTERNAL_SELF_MANAGED, ROUND_ROBIN)
google_compute_health_check (gRPC, port 50052)
data.google_compute_network_endpoint_group
```

Provider-schema inspection also confirmed native resources for project API
enablement, networking/firewalls, GKE and node pools, Artifact Registry, and IAM
bindings. **This checks provider coverage and HCL validity, not Google Cloud
API acceptance or runtime operation.** [12]

Avoid the Terraform/GitOps dependency cycle with an explicit staged bootstrap:

1. Terraform provisions the platform, IAM, mesh, and server endpoint policy;
   Argo CD is bootstrapped against the GitOps repository.
2. Argo CD creates the Service and workloads; the NEG controller creates the
   named zonal NEG and registers endpoints.
3. Wait for NEG existence, then apply the backend/route Terraform stack using
   the NEG data source. No fixed sleeps or routine `terraform -target`.
4. Wait for accepted client/server xDS configuration and healthy endpoints,
   then run the demonstration client.

The health listener must not falsely report ready before the application xDS
listener can serve. Teardown must detach cloud backend references before
deleting the Service/NEGs or cluster to prevent leaked resources. [4]

## 6. Confirmation boundary and remaining gates

**Confirmed locally:** a documented GKE/NEG integration, outbound and inbound
xDS APIs, released version requirements, bootstrap fields, native Terraform
schema coverage, container builds, and rendered/schema-checked GitOps resources.
Mock-provider Terraform tests do not contact GCP. The Go integration test uses
an ADS fixture and real xDS servers to demonstrate pre-configuration refusal,
multiple backends, and an endpoint addition on an existing channel. That fixture
is test-only and does not replace Google's managed control plane.

**Deployment in progress:** a project and location have been selected and
billable provisioning approved. Image publication, deployment screenshots, and
a live Cloud Service Mesh connection are not yet confirmed. Follow the
[infrastructure runbook](https://github.com/cohavisi/utila-gRPC-Infrastructure#readme).
Before considering the deployment complete, verify:

- Billing/quota, the selected GKE version, required permissions, and enabled APIs.
- Image publication and the actual running image/bootstrap digests.
- Server Listener acceptance, endpoint-policy matching, and non-serving
  behavior when configuration is absent.
- Client route/endpoint acceptance and successful calls to multiple pods.
- Endpoint changes after scaling and recovery after failure.

If a live gate fails, investigate the chosen configuration instead of silently
substituting DNS dialing, a plain non-xDS server, an additional local control
plane, or a traffic-proxy sidecar.

## References

1. [Cloud Service Mesh: service-routing proxyless setup](https://docs.cloud.google.com/service-mesh/docs/service-routing/set-up-proxyless-mesh)
2. [Cloud Service Mesh: proxyless overview](https://docs.cloud.google.com/service-mesh/docs/service-routing/proxyless-overview)
3. [Cloud Service Mesh: proxyless service security, including GKE and endpoint policies](https://docs.cloud.google.com/service-mesh/docs/service-routing/security-proxyless-setup)
4. [GKE: standalone NEGs, controller ownership, and teardown](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg)
5. [Cloud Service Mesh: Gateway API preparation](https://docs.cloud.google.com/service-mesh/docs/gateway/prepare-gateway)
6. [Google beta provider 8.12.0: fleet membership implementation](https://github.com/hashicorp/terraform-provider-google-beta/blob/v8.12.0/google-beta/services/gkehub/resource_gke_hub_feature_membership.go)
7. [grpc-go 1.85.0 module and Go requirement](https://github.com/grpc/grpc-go/blob/v1.85.0/go.mod)
8. [grpc-go 1.85.0: xDS server implementation](https://github.com/grpc/grpc-go/blob/v1.85.0/xds/server.go)
9. [EndpointPolicy API: GRPC_SERVER, metadata matching, and optional TLS](https://docs.cloud.google.com/service-mesh/docs/reference/network-services/rest/v1/projects.locations.endpointPolicies)
10. [Bootstrap generator 0.20.0: flags and generated configuration](https://github.com/GoogleCloudPlatform/traffic-director-grpc-bootstrap/blob/v0.20.0/main.go)
11. [Cloud Service Mesh: onboarding, xDS, and runtime IAM](https://docs.cloud.google.com/service-mesh/docs/onboarding/prepare-service-routing-envoy-proxyless)
12. [Google provider 8.12.0 resource documentation](https://registry.terraform.io/providers/hashicorp/google/8.12.0/docs)
