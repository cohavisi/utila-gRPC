# Part 1, Task 1: Analysis of gRPC Load-Balancing Issues

## Summary

The central problem is a mismatch between **connection-level load balancing**
and **RPC-level traffic**. gRPC commonly sends many concurrent calls over
long-lived HTTP/2 connections, while a standard Kubernetes Service selects a
backend per TCP connection. Consequently, a busy client can keep sending calls to
one pod even when other replicas have spare capacity.

DNS is a discovery mechanism, not a per-request load balancer. Whether this
problem occurs depends on what DNS returns, how the client selects backends, and
where load balancing takes place.

## 1. Why gRPC, HTTP/2, and DNS discovery can produce uneven traffic

### A standard Kubernetes Service hides individual backends

Assume a Go client calls a gRPC service with three server pods through a normal,
non-headless ClusterIP Service. DNS resolves the Service name to its virtual IP,
not to the individual pod IPs. [1]

```text
DNS: greeter.default.svc.cluster.local -> 10.96.10.20 (Service IP)

Go client -- TCP connection --> Service IP -- selected backend --> Pod B1
                                                                Pod B2
                                                                Pod B3
```

In the usual Layer 4 Service data path, a backend is selected when a TCP
connection is established. Subsequent traffic on that connection continues to
the same backend; Kubernetes does not choose a different pod for every RPC. [2]

### HTTP/2 allows many calls to share that connection

Each gRPC call uses an HTTP/2 stream, and multiple streams can be multiplexed
over the same TCP connection. Reusing the connection avoids repeated connection
setup, but all those calls still reach its selected pod.

```text
One long-lived connection to B1:
  RPC 1 ----\
  RPC 2 -----+----> Pod B1
  RPC 3 ----/

Pod B2 and Pod B3 receive none of these calls.
```

A gRPC channel is a logical abstraction, not necessarily exactly one TCP
connection: it can manage multiple connections according to its configuration.
The problematic case is a client whose traffic is concentrated on one or a few
backend connections. Many independent clients can improve distribution, but
balancing connection counts still does not guarantee balanced request rates or
resource usage. [2][3]

### Returning multiple DNS addresses is not sufficient either

A headless Service (`clusterIP: None`) allows DNS to return individual backend
pod addresses. However, the client must actually use those addresses to balance
calls. gRPC's default `pick_first` policy uses a reachable backend rather than
spreading calls across all returned addresses. `round_robin`, by contrast,
distributes new RPCs across ready backend connections. [1][4]

DNS changes also do not push updates into existing connections. Endpoint
discovery depends on resolver behavior and caching, and learning about another
address does not move an established TCP connection to that address. A lower
DNS TTL alone therefore does not fix connection pinning.

## 2. Operational issues and Kubernetes examples

### Issue 1: Hotspots despite sufficient total capacity

**Example:** One client sends 900 unary RPCs per second over one connection.
Three server pods can each handle 400 RPCs per second for this workload. If the
connection selects B1, B1 receives all 900 RPCs per second while B2 and B3 remain
idle.

Although total capacity is 1,200 RPCs per second, the application cannot use it
effectively. Requests queue, tail latency rises, and calls can exceed their
deadlines. Depending on the workload, resource exhaustion can also cause errors
or restarts.

The key distinction is **available cluster capacity versus capacity reachable
through the client's current backend selection**.

### Issue 2: Scale-out does not redistribute existing connections

**Example:** A Deployment grows from two replicas to six during a traffic spike.
The new pods become ready and eligible for new connections, but existing client
connections remain associated with the original two pods.

Adding replicas may provide little immediate relief if clients keep reusing
those connections. New capacity exists without receiving a proportional share
of traffic. Restarting clients might redistribute connections, but that is a
disruptive workaround rather than a reliable load-balancing strategy.

For a normal ClusterIP Service, the DNS answer can remain unchanged throughout
this scale-out: the Service IP stays the same while its backend set changes.
Repeated DNS lookups therefore would not reveal the new pods to the client.

### Issue 3: Concentrated failure impact and deployment disruption

**Example:** B1 crashes while many concurrent calls share a connection to it.
Those in-flight calls can fail together. New calls may also be delayed or fail
while the client detects the broken connection and reconnects.

During a rolling deployment, removing a pod from eligibility for new Service
connections does not migrate existing connections or finish in-flight calls.
Without application-level draining and a sufficient termination grace period,
pod termination can interrupt work. gRPC graceful shutdown allows in-flight
RPCs to finish while directing clients away from new calls on the draining
server. [5]

These are not failures unique to gRPC, but multiplexing many calls onto a small
number of connections concentrates their impact.

### Issue 4: Long-lived streaming RPCs remain pinned

**Example:** A pod handles a long-lived bidirectional stream carrying substantial
traffic. Adding replicas cannot move part of that existing stream to another
pod.

Even a correct per-RPC balancer selects a backend when the RPC starts; it does
not rebalance individual messages within an established streaming RPC. Streams
cannot be transparently migrated between backends. Applications that need
reconnection must define how to resume work safely. [3]

## 3. Why HTTP/1.1 often shows less imbalance

The claim that HTTP/1.1 "does not suffer from this" needs
qualification: **HTTP/1.1 is not immune to connection pinning**.

HTTP/1.1 does not provide HTTP/2-style multiplexing of independent concurrent
streams. Clients commonly use multiple connections to support concurrent work
and avoid response head-of-line blocking. Each connection gives a Layer 4
balancer another opportunity to select a backend. HTTP/1.1 pipelining does not
provide equivalent multiplexing because responses must remain ordered. [6]

| Property | Typical HTTP/1.1 client | Typical gRPC client |
| --- | --- | --- |
| Concurrent work | Often uses a pool of connections | Can multiplex many RPCs on one connection |
| Backend selection at Layer 4 | Once per connection | Once per connection |
| Potential consequence | More connections create more distribution opportunities | Few connections can concentrate a large RPC volume |

HTTP/1.1 persistent connections can still remain attached to one backend, and a
small or unevenly used pool can still produce hotspots. The difference is the
connection usage pattern, not a protocol-level fairness guarantee. HTTP/2
services with appropriate client-side or Layer 7 balancing avoid this problem
without giving up multiplexing.

## 4. Implications for the solution

The architectural requirement is to **select among backend endpoints for new
RPCs**, rather than relying only on connection-level balancing:

- **Client-side balancing:** combine headless-Service discovery with a Go gRPC
  balancing policy such as `round_robin`. Neither headless DNS alone nor
  `round_robin` against a single ClusterIP exposes and balances the individual
  pods. Account for endpoint refresh and the number of backend connections.
- **gRPC-aware Layer 7 proxy:** use a proxy such as Envoy, configured to discover
  individual backends and distribute RPCs across them. Merely forwarding all
  calls through one upstream connection to a ClusterIP retains the bottleneck.
- **Proxyless xDS:** let a compatible gRPC client receive endpoint and supported
  routing/load-balancing configuration from a control plane, then select
  backends directly. This separates centralized configuration from the
  application data path. [4]

Whichever approach is chosen, pair it with graceful shutdown, RPC deadlines,
and bounded retries only where replay is safe. Observe per-pod RPC rates,
active streams, latency, and errors; aggregate resource utilization alone can
hide hotspots. Equal RPC counts are not necessarily equal work, so validate
the policy against the actual workload.

The detailed comparison and GCP architecture recommendation belong in Task 2.

## References

1. [Kubernetes: DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#services)
   - Normal Service records resolve to the Service IP; headless Service records
     expose backend pod addresses.
2. [Google Cloud: Using Envoy Proxy to load-balance gRPC services on GKE](https://docs.cloud.google.com/kubernetes-engine/docs/tutorials/exposing-grpc-services-on-gke-using-envoy-proxy)
   - Explains HTTP/2 multiplexing, Layer 4 connection balancing, and Envoy's
     per-RPC balancing with headless-Service discovery.
3. [gRPC: Performance Best Practices](https://grpc.io/docs/guides/performance/)
   - Describes channel reuse, multiple connections per channel, and why an
     established streaming RPC cannot be rebalanced.
4. [gRPC: Custom Load Balancing Policies](https://grpc.io/docs/guides/custom-load-balancing/)
   - Documents resolver/balancer responsibilities, default `pick_first`,
     `round_robin`, and configuration through xDS.
5. [gRPC: Graceful Shutdown](https://grpc.io/docs/guides/server-graceful-stop/)
   - Explains draining in-flight RPCs and bounding shutdown time.
6. [RFC 9112, Sections 9.3-9.4: Persistence, Pipelining, and Concurrency](https://www.rfc-editor.org/rfc/rfc9112.html#section-9.3)
   - Establishes HTTP/1.1 persistent-connection behavior, ordered pipelined
     responses, and the use of multiple connections for concurrency.
