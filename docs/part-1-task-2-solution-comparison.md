# Part 1, Task 2: Solution Comparison and GCP Recommendation

## Executive recommendation

For the stated problem of uneven internal Go-to-Go gRPC traffic in Kubernetes,
**start with client-side `round_robin` balancing and headless-Service discovery**.
Under the single-cluster assumptions below, this fixes the connection-pinning
problem without introducing proxies or a mesh control plane.

If the requirement extends to **centrally managed discovery and traffic policies
without sidecars**, my preferred managed-mesh option on GCP is **Cloud Service
Mesh with proxyless gRPC**. It supplies dynamic configuration to the Go gRPC
library through xDS, while RPCs travel directly between applications.

These are different scopes, not competing claims that one product is always
best. A mesh is not necessary merely to distribute unary RPCs. Conversely, DNS
plus round robin is not a substitute for a centrally managed mesh.

**Part 2 uses Cloud Service Mesh with proxyless gRPC to demonstrate centrally
managed discovery and policy beyond the minimal DNS-based fix.** This document
evaluates the architecture; the completed deployment, measured results, and screenshots are
recorded separately in the
[live evidence](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md).

## 1. Assumptions and decision criteria

The initial recommendation assumes one GKE cluster, internal gRPC traffic,
control over the Go clients, and permission to update their dependencies and
configuration. The initial scope does not establish a need for multi-region
failover, centralized canary routing, or uniform mesh-wide security policies.
Those are decision-changing requirements, not assumptions to introduce silently.

Building on [Task 1](part-1-task-1-grpc-load-balancing.md), a suitable solution must
select backends for **new RPCs**, discover changes as pods scale, and handle
backend failures. Existing streaming RPCs remain attached to their selected
backends; no option below transparently redistributes messages from one
established stream across pods.

The comparison considers application changes, endpoint discovery, policy
capabilities, operational ownership, resource and monetary costs, and GCP
integration. Performance and cost rankings are qualitative: they are not
benchmark results.

## 2. Comparison

| Approach | How it fixes balancing and discovery | Application impact | Main tradeoff |
| --- | --- | --- | --- |
| Go client + headless DNS | DNS exposes pod IPs; the client balances new RPCs across ready connections | Configure DNS resolver and `round_robin` | Least additional infrastructure; DNS refresh and client configuration remain your responsibility |
| Linkerd | Sidecar proxies discover Service endpoints and perform latency-aware per-request balancing | Usually no gRPC code changes | Adds a mesh control plane and per-pod proxies |
| Standalone Envoy | An L7 proxy discovers individual backends and balances RPCs | Point clients at a shared proxy, or inject sidecars | Flexible, but the team owns proxy configuration, availability, and discovery |
| Managed Cloud Service Mesh + Envoy | Managed control plane configures proxy-based traffic management | Usually no gRPC code changes; enroll workloads | Broader protocol support; proxies still consume workload resources |
| Managed Cloud Service Mesh + proxyless gRPC | Go libraries receive endpoints and policies through xDS and balance directly | xDS registration, bootstrap, target, credentials, and instrumentation | No sidecar hop; features depend on both client and control-plane support |
| Istio ambient + L7 waypoints | Node proxies provide L4 transport; shared Envoy waypoints provide L7 balancing | Enroll workloads and configure waypoints | Sidecar-free, not proxyless; still requires a mesh and shared proxy capacity |

### A. Go client changes: the smallest targeted fix

Use a selector-based headless Service (`clusterIP: None`) so its DNS name exposes
backend pod addresses. Configure the Go client to use a `dns:///` target and
`round_robin`, rather than the default `pick_first` policy. The client can then
reuse connections to multiple backends while choosing a ready connection for
each new RPC. [1][2]

Both pieces matter. Headless DNS alone does not change `pick_first`;
`round_robin` against a single ClusterIP does not expose the individual pods.
"Exposing" pod IPs here means making them discoverable within the cluster, not
publishing them on the internet.

**Advantages:** no proxy process, no additional mesh service to operate, and
limited application changes. It is a good first choice when the requirement is
fairer distribution of similar-cost unary calls.

**Limitations:** DNS resolution and caching determine when clients learn
endpoint changes; a TTL is not a guarantee of application refresh timing.
Client configuration must remain consistent across services. Connecting many
clients to many replicas also increases connection count. Round robin balances
calls, not CPU usage or request cost. TLS, authorization, retries, and telemetry
still need deliberate application/platform configuration.

### B. Linkerd: transparent Kubernetes-native mesh

Linkerd's outbound proxy discovers Kubernetes Service endpoints and balances
HTTP/2 and gRPC requests using a latency-aware EWMA algorithm. Its documented
architecture uses Rust sidecar proxies, not Envoy. The mesh also provides
identity-based proxy-to-proxy mTLS and traffic metrics. [3][4]

**Advantages:** avoids changing every client library to implement discovery and
balancing. It is attractive when a team wants transparent mesh behavior across
applications rather than a Go-only solution.

**Limitations:** operating the mesh includes its control plane, certificate
trust configuration, upgrades, and proxy resources. It does not satisfy a
strict no-sidecar requirement.

Importantly, do not copy the headless-Service recipe from option A into Linkerd
without understanding the difference. Linkerd documents that a destination pod
IP is treated as a direct destination, not a Service to rebalance. Its
Service-level balancing expects a discoverable Service destination. [3]

### C. Envoy: capable proxy, not a complete mesh by itself

Envoy understands HTTP/2 streams and can balance individual gRPC calls across
upstream endpoints. Google's GKE example uses headless-Service discovery so the
proxy sees pod IPs rather than a single Service IP. [5]

It can run as a **shared proxy tier** or as **per-workload sidecars**. A shared
tier avoids per-pod sidecars but still proxies every call. Sidecars distribute
the proxy workload, but add a proxy lifecycle and resource allocation to each
enrolled application pod.

**Advantages:** rich L7 routing, health checking, and policy capabilities;
clients do not need xDS support when Envoy consumes configuration for them.

**Limitations:** standalone Envoy does not automatically provide a complete
service registry, certificate authority, or managed control plane. The team
must choose and operate those integrations. Shared proxies need redundancy,
capacity planning, and monitoring; few long-lived client connections can also
produce uneven utilization across the proxy tier.

The proxy must discover individual backends. Forwarding everything over one
upstream connection to a ClusterIP merely relocates connection pinning.

### D. Managed service mesh on GCP: Cloud Service Mesh

Cloud Service Mesh provides a managed control plane and supports different data
planes and configuration APIs. The product overview distinguishes Istio-API
deployments with Envoy from Google Cloud API configurations that can use Envoy
or proxyless gRPC. Managed control-plane ownership does not imply a proxy-free
data plane. [6]

**With Envoy**, it is a better fit when transparent integration, non-gRPC
protocols, or proxy features absent from the Go client are required. It reduces
control-plane maintenance relative to operating a mesh independently, but
proxies still need resources, rollout planning, and capacity monitoring.

**With proxyless Go gRPC**, it provides centralized endpoint and supported
traffic-policy updates without a sidecar process. This is the preferred
managed-mesh choice for a Go-centric fleet that specifically needs those
capabilities and accepts the feature constraints discussed below. [7][8]

The tradeoffs are GCP configuration/IAM ownership, client dependency upgrades,
and product-specific integration. xDS is an open protocol, but that does not
make Google Cloud routing resources portable to another vendor unchanged.
Cloud Service Mesh also has client-based pricing that explicitly includes
proxyless gRPC instances; removing sidecars does not remove service charges.
Include GKE capacity and observability costs in the estimate. [9]

### E. Additional alternative: Istio ambient with waypoints

Ambient mode separates node-level L4 proxies (`ztunnel`) from shared Envoy
waypoints providing L7 processing. Istio documents HTTP routing and load
balancing as waypoint functions. A node-level L4 proxy alone does not solve
per-RPC balancing for a long-lived gRPC connection. [10]

**Advantages:** no per-application sidecars and less coupling to Go client
libraries; useful for a broader, mixed-protocol platform.

**Limitations:** sidecar-free is not proxyless. Calls still traverse mesh
proxies, and waypoints need operational ownership and capacity. This is more
infrastructure than the stated balancing-only requirement needs. Do not assume
that upstream Istio ambient capabilities are automatically supported by a
particular managed Cloud Service Mesh release.

## 3. Decision for this GCP workload

**Choose headless DNS plus Go `round_robin` for the assumed single-cluster,
balancing-only requirement.** GKE already provides the necessary discovery
primitive, the clients are under our control, and no requirement yet justifies
adding a proxy tier or mesh control plane. Validate endpoint refresh under
scale-out before treating this as sufficient for a production SLO.

Compared with that baseline, Linkerd and standalone Envoy add runtime components
to solve a problem the client can solve directly. Ambient removes sidecars but
not the proxy infrastructure. Managed Cloud Service Mesh earns its additional
configuration and cost when centralized policy, managed discovery, or
cross-environment traffic management becomes a real requirement.

**If a managed mesh is required, prefer Cloud Service Mesh with proxyless gRPC**
for this Go-centric fleet: it combines a GCP-managed control plane, dynamic xDS
updates, and direct application-to-application traffic. Prefer the Envoy-based
option instead if required protocols or policies exceed the supported
proxyless feature set. Prefer Linkerd when transparent Kubernetes-native
integration and its operating model matter more than avoiding sidecars.

This decision deliberately separates the minimal operational fix from the
broader architecture exercised in Part 2.

## 4. How sidecar-free Go gRPC works with xDS

xDS is a family of configuration APIs originally associated with Envoy.
Compatible gRPC libraries implement a subset directly, allowing the Go process
to act as both application client and mesh data plane. [7][11]

```text
                         Configuration path
Cloud Service Mesh ---------------- xDS ----------------> Go client
                                                            |
                         Application traffic                |
                                            +---------------+---------------+
                                            |               |               |
                                            v               v               v
                                         Pod B1          Pod B2          Pod B3
                                         :50051          :50051          :50051
```

The control plane supplies configuration; it does not forward `SayHello` calls.
The client selects a backend using the received policy and sends the RPC
directly. Endpoint updates can affect subsequent calls without changing their
logical target. This does not imply instantaneous updates or migration of
existing streams.

### Application and platform responsibilities

1. **Register xDS support.** Import `google.golang.org/grpc/xds` in the Go client
   to register the resolver and balancers. Pin a maintained `grpc-go` release
   supporting the selected features, rather than choosing an old release just
   because it first introduced xDS. [11][12]
2. **Provide bootstrap configuration.** Set `GRPC_XDS_BOOTSTRAP` to a mounted
   configuration file describing the control-plane endpoint, credentials, and
   client identity. A bootstrap generator can run as an init container: it
   finishes before the application starts and is not a traffic-proxy sidecar.
   Cloud Service Mesh supports xDS v3 only. [12][13]
3. **Configure identity and connectivity.** Give the intended workload identity
   the required control-plane permissions, including the appropriate
   `roles/trafficdirector.client` access. Use narrowly scoped identity bindings,
   not a project-wide grant to every workload. Ensure clients can reach both
   the control plane and backend pod addresses.
4. **Use an xDS target.** The channel target identifies a configured logical
   service, for example
   `xds:///greeter.default.svc.cluster.local:50051` for the documented GKE
   Gateway API model. The URI is not enough on its own: bootstrap and matching
   control-plane routes must also exist. [14]
5. **Register endpoints and policy.** Configure the chosen integration to keep
   backend membership and health current. With standalone GKE NEGs, the NEG
   controller tracks pod IP/port membership, while backend services and mesh
   routing still require configuration. A Kubernetes Deployment alone does not
   create a working Google Cloud routing graph. [15]
6. **Keep server and client roles distinct.** The caller chooses backends.
   Server-side xDS can supply supported inbound listener/security configuration;
   it does not make the server distribute incoming calls to other pods.
   Implement the xDS server integration separately from the
   client's balancing logic.

### Choose one GCP configuration model

Google's documentation includes multiple paths that must not be mixed into one
unqualified recipe. [8][16][18]

| Configuration path | Important distinction |
| --- | --- |
| Google Cloud service-routing APIs | Google Cloud `Mesh` and route resources configure supported xDS clients. These are cloud resources, not Kubernetes Gateway API objects. |
| GKE Gateway API integration | Fleet/cluster configuration, Kubernetes Services and routes, and bootstrap setup. The current guide requires fleet configuration using `gateway`, supports FQDN targets, and documents no multi-cluster discovery/balancing for these clusters. |
| Legacy load-balancing APIs | Older examples use forwarding rules, target gRPC proxies, and URL maps. Google recommends service-routing APIs over new deployments using this legacy model. |

The current GKE proxyless guide lists Go gRPC 1.68.0 as its minimum; that is a
guide-specific floor, not a recommendation to deploy that release today.
Gateway onboarding currently shows `gcloud alpha` fleet configuration commands.
Confirm release-channel availability, support status, and IaC coverage for the
chosen path before implementation; do not infer general availability merely
from the presence of a tutorial. [8][14][16][17]

For Part 2, the GKE Gateway API path is a documented candidate aligned with
GitOps. Subsequent [integration confirmation](part-2-tasks-3-6-integration-decision.md)
selected Google Cloud service-routing APIs with GKE NEGs instead, based on native
Terraform coverage and the explicit server-side xDS endpoint-policy mechanism.
The decision records verification boundaries and live deployment findings.
These configuration models are not interchangeable.

## 5. Proxyless capabilities and limitations

Supported functionality is the **intersection of the control-plane API,
integration path, and pinned Go library version**. A feature in Envoy or
upstream `grpc-go` is not automatically supported by Cloud Service Mesh in the
same configuration. [8][11][17]

| Area | Assessment |
| --- | --- |
| Discovery and balancing | Dynamic endpoint configuration and per-RPC backend selection are core capabilities. Round robin is a conservative documented starting policy; do not assume all Envoy balancing algorithms are supported. |
| Routing and resilience | The Google Cloud API feature matrix lists traffic splitting, matching, retries, and outlier detection for proxyless gRPC, with constraints. Validate each feature against the selected API path and Go version. |
| Feature parity | The matrix does not offer proxyless parity for traffic mirroring, header transformations, redirects, or URI rewrites. Retry support excludes per-retry timeout, and circuit breaking is limited to `maxRequests`. |
| Security | The matrix lists TLS/mTLS and authorization for proxyless GKE workloads, but they require the documented credentials, certificate provisioning, server support, and policies. Registering the xDS resolver does not automatically secure application traffic. |
| Telemetry | Cloud Service Mesh cannot configure the application's logging/tracing merely through xDS. Integrate supported Go/OpenTelemetry instrumentation and verify export; do not promise proxy-style telemetry without application work. |
| Library compatibility | Unsupported xDS fields may be ignored; unsupported values or policies can cause configuration rejection. Pin compatible versions and observe accepted configuration, not just successful resource creation. |
| Failure behavior | Test cold-start control-plane unavailability separately from an already-running client's disconnection. Do not assume an unconfigured client can discover backends during an outage. |

Authentication to the xDS server and TLS/mTLS for application RPCs are separate
concerns. The former grants configuration access; it does not prove that the
`SayHello` traffic is encrypted or that callers are authorized.

## 6. Part 2 evidence and acceptance checks

The deployment should demonstrate the architectural claims rather than only
show that pods are running:

- Send repeated unary calls through a reused client channel; record the
  serving pod identity and per-pod call counts.
- Scale replicas without restarting the client; observe when new healthy
  endpoints begin receiving calls.
- Terminate a backend gracefully and test an abrupt failure separately;
  observe errors, deadlines, and recovery.
- Confirm xDS configuration is accepted and application pods have no
  long-running traffic-proxy sidecar. A successful call alone does not prove
  xDS was used.
- Verify the selected transport security and telemetry explicitly. Limit
  retries to safe operations and document long-lived stream behavior.

These checks guided Part 2. See the
[completed deployment evidence](https://github.com/cohavisi/utila-gRPC-Infrastructure/blob/main/docs/deployment-evidence.md)
for the exact scenarios observed, startup failures, screenshots, and untested
scope; the checklist itself is not a claim that every possible failure or
security configuration was tested.

## References

Official documentation reviewed on 2026-11-19. Product integration and feature
tables evolve; the implementation should record the actual pinned versions and
chosen API path.

1. [Kubernetes: DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#services)
   - Normal versus headless-Service discovery.
2. [gRPC: Custom Load Balancing Policies](https://grpc.io/docs/guides/custom-load-balancing/)
   - Resolver/balancer responsibilities, `pick_first`, and `round_robin`.
3. [Linkerd: Load Balancing](https://linkerd.io/docs/features/load-balancing/)
   - EWMA, Kubernetes endpoint discovery, and headless-Service limitations.
4. [Linkerd: Architecture](https://linkerd.io/docs/reference/architecture/)
   - Rust sidecars, control-plane components, identity, and proxy metrics.
5. [Google Cloud: Envoy-based gRPC load balancing on GKE](https://docs.cloud.google.com/kubernetes-engine/docs/tutorials/exposing-grpc-services-on-gke-using-envoy-proxy)
   - Per-RPC proxy balancing and headless discovery of backend pods.
6. [Google Cloud: Cloud Service Mesh overview](https://docs.cloud.google.com/service-mesh/docs/overview)
   - Managed control plane, data-plane choices, and configuration API families.
7. [Google Cloud: Proxyless gRPC overview](https://docs.cloud.google.com/service-mesh/docs/service-routing/proxyless-overview)
   - Direct application traffic, xDS configuration, and deployment patterns.
8. [Google Cloud: Google Cloud API supported features](https://docs.cloud.google.com/service-mesh/docs/service-routing/features)
   - Proxyless/proxy feature differences and guidance against new legacy-API deployments.
9. [Google Cloud: Cloud Service Mesh pricing](https://cloud.google.com/service-mesh/pricing)
   - Client-based billing, including proxyless instances.
10. [Istio: Configure waypoint proxies](https://istio.io/latest/docs/ambient/usage/waypoint/)
    - Ambient L4 versus L7 responsibilities and shared waypoints.
11. [gRPC: xDS features by language and version](https://github.com/grpc/grpc/blob/master/doc/grpc_xds_features.md)
    - Go compatibility, security mechanisms, and unsupported-configuration behavior.
12. [Google Cloud: Prepare Envoy and proxyless workloads](https://docs.cloud.google.com/service-mesh/docs/onboarding/prepare-service-routing-envoy-proxyless)
    - Bootstrap, Go xDS registration, IAM, targets, and library maintenance.
13. [Google Cloud: xDS control-plane APIs](https://docs.cloud.google.com/service-mesh/docs/service-routing/xds-control-plane-apis)
    - xDS v3 and client permissions.
14. [Google Cloud: Proxyless gRPC service mesh on GKE](https://docs.cloud.google.com/service-mesh/docs/gateway/proxyless-grpc-mesh)
    - GKE Gateway API example, Go requirements, and bootstrap options.
15. [Google Cloud: Standalone GKE NEGs](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/standalone-neg)
    - Pod endpoint registration and ownership of associated cloud resources.
16. [Google Cloud: Prepare Gateway API for Cloud Service Mesh](https://docs.cloud.google.com/service-mesh/docs/gateway/prepare-gateway)
    - Fleet setup, FQDN requirement, alpha configuration commands, and multi-cluster limitations.
17. [Google Cloud: Proxyless gRPC limitations](https://docs.cloud.google.com/service-mesh/docs/service-routing/limitations-proxyless)
    - Feature restrictions and API-specific caveats.
18. [Google Cloud: Service-routing APIs overview](https://docs.cloud.google.com/service-mesh/docs/service-routing/service-routing-overview)
    - Cloud `Mesh` and route resources, distinct from Kubernetes Gateway API resources.
