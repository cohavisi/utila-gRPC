package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	greeterv1 "github.com/cohavisi/utila-gRPC/api/greeter/v1"
	"github.com/cohavisi/utila-gRPC/internal/greeter"
	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listenerv3 "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	routev3 "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	routerv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/router/v3"
	hcmv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/xds"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// This ADS server validates gRPC's xDS protocol behavior, not Google's managed control plane.
func TestXDSDiscoveryAndLiveEndpointUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	controlListener := listen(t)
	cache := cachev3.NewSnapshotCache(false, cachev3.IDHash{}, nil)
	control := grpc.NewServer()
	discoveryv3.RegisterAggregatedDiscoveryServiceServer(control, serverv3.NewServer(ctx, cache, nil))
	go func() { _ = control.Serve(controlListener) }()
	t.Cleanup(control.Stop)

	bootstrap, err := json.Marshal(map[string]any{
		"xds_servers": []any{map[string]any{
			"server_uri":      controlListener.Addr().String(),
			"channel_creds":   []any{map[string]any{"type": "insecure"}},
			"server_features": []string{"xds_v3"},
		}},
		"node":                                   map[string]any{"id": "local-integration"},
		"server_listener_resource_name_template": "grpc/server?xds.resource.listening_address=%s",
	})
	if err != nil {
		t.Fatal(err)
	}

	var serverListeners []cachetypes.Resource
	var endpoints []*endpointv3.LbEndpoint
	var readyChannels []chan struct{}
	for i := 1; i <= 3; i++ {
		listener := listen(t)
		ready := make(chan struct{}, 1)
		readyChannels = append(readyChannels, ready)
		server, err := xds.NewGRPCServer(
			xds.BootstrapContentsForTesting(bootstrap),
			xds.ServingModeCallback(func(_ net.Addr, change xds.ServingModeChangeArgs) {
				if change.Mode == connectivity.ServingModeServing {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		greeterv1.RegisterGreeterServer(server, &greeter.Service{ServerID: fmt.Sprintf("server-%d", i)})
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		serverListeners = append(serverListeners, inboundListener(t, listener.Addr().String()))
		endpoints = append(endpoints, &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{Address: socketAddress(t, listener.Addr().String())},
			},
		})

		if i == 1 {
			conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			beforeConfig, stop := context.WithTimeout(ctx, 200*time.Millisecond)
			_, callErr := greeterv1.NewGreeterClient(conn).SayHello(beforeConfig, &greeterv1.HelloRequest{Name: "reviewer"})
			stop()
			_ = conn.Close()
			if callErr == nil {
				t.Fatal("xDS server served a request without an accepted Listener configuration")
			}
		}
	}

	publish := func(version string, endpointCount int) {
		t.Helper()
		routes := &routev3.RouteConfiguration{
			Name: "greeter-routes",
			VirtualHosts: []*routev3.VirtualHost{{
				Name: "greeter", Domains: []string{"greeter.test"},
				Routes: []*routev3.Route{{
					Match: &routev3.RouteMatch{PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: ""}},
					Action: &routev3.Route_Route{Route: &routev3.RouteAction{
						ClusterSpecifier: &routev3.RouteAction_Cluster{Cluster: "greeter-cluster"},
					}},
				}},
			}},
		}
		outbound := &hcmv3.HttpConnectionManager{
			RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{RouteConfig: routes},
			HttpFilters:    httpFilters(t),
		}
		listeners := append([]cachetypes.Resource{}, serverListeners...)
		listeners = append(listeners, &listenerv3.Listener{
			Name:        "greeter.test",
			ApiListener: &listenerv3.ApiListener{ApiListener: typed(t, outbound)},
		})
		snapshot, err := cachev3.NewSnapshot(version, map[resourcev3.Type][]cachetypes.Resource{
			resourcev3.ListenerType: listeners,
			resourcev3.ClusterType: {&clusterv3.Cluster{
				Name:                 "greeter-cluster",
				ClusterDiscoveryType: &clusterv3.Cluster_Type{Type: clusterv3.Cluster_EDS},
				EdsClusterConfig:     &clusterv3.Cluster_EdsClusterConfig{EdsConfig: adsConfig()},
				LbPolicy:             clusterv3.Cluster_ROUND_ROBIN,
				ConnectTimeout:       durationpb.New(2 * time.Second),
			}},
			resourcev3.EndpointType: {&endpointv3.ClusterLoadAssignment{
				ClusterName: "greeter-cluster",
				Endpoints: []*endpointv3.LocalityLbEndpoints{{
					Locality:            &corev3.Locality{Region: "local", Zone: "test"},
					LoadBalancingWeight: wrapperspb.UInt32(1),
					LbEndpoints:         endpoints[:endpointCount],
				}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := snapshot.Consistent(); err != nil {
			t.Fatalf("invalid xDS test snapshot: %v", err)
		}
		if err := cache.SetSnapshot(ctx, "local-integration", snapshot); err != nil {
			t.Fatal(err)
		}
	}
	publish("1", 2)
	for _, ready := range readyChannels {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("server did not accept its xDS Listener:", ctx.Err())
		}
	}

	builder, err := xds.NewXDSResolverWithConfigForTesting(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient("xds:///greeter.test",
		grpc.WithResolvers(builder), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := greeterv1.NewGreeterClient(conn)
	observe := func(want int) map[string]int {
		t.Helper()
		observeCtx, stopObserving := context.WithTimeout(ctx, 10*time.Second)
		defer stopObserving()
		seen := map[string]int{}
		for observeCtx.Err() == nil && len(seen) < want {
			callCtx, stop := context.WithTimeout(observeCtx, 5*time.Second)
			reply, err := client.SayHello(callCtx, &greeterv1.HelloRequest{Name: "reviewer"}, grpc.WaitForReady(true))
			stop()
			if err != nil {
				t.Fatalf("SayHello through xDS: %v", err)
			}
			seen[reply.GetServerId()]++
			time.Sleep(time.Millisecond)
		}
		if len(seen) != want {
			t.Fatalf("wanted %d discovered backends; got %v", want, seen)
		}
		return seen
	}
	if seen := observe(2); seen["server-3"] != 0 {
		t.Fatalf("server-3 was used before being advertised in EDS: %v", seen)
	}
	publish("2", 3)
	if seen := observe(3); seen["server-3"] == 0 {
		t.Fatalf("the existing channel did not adopt the new EDS endpoint: %v", seen)
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func typed(t *testing.T, message proto.Message) *anypb.Any {
	t.Helper()
	value, err := anypb.New(message)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func socketAddress(t *testing.T, address string) *corev3.Address {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return &corev3.Address{Address: &corev3.Address_SocketAddress{
		SocketAddress: &corev3.SocketAddress{
			Address: host, PortSpecifier: &corev3.SocketAddress_PortValue{PortValue: uint32(port)},
		},
	}}
}

func adsConfig() *corev3.ConfigSource {
	return &corev3.ConfigSource{
		ResourceApiVersion: corev3.ApiVersion_V3,
		ConfigSourceSpecifier: &corev3.ConfigSource_Ads{
			Ads: &corev3.AggregatedConfigSource{},
		},
	}
}

func httpFilters(t *testing.T) []*hcmv3.HttpFilter {
	return []*hcmv3.HttpFilter{{
		Name:       "envoy.filters.http.router",
		ConfigType: &hcmv3.HttpFilter_TypedConfig{TypedConfig: typed(t, &routerv3.Router{})},
	}}
}

func inboundListener(t *testing.T, address string) *listenerv3.Listener {
	t.Helper()
	hcm := &hcmv3.HttpConnectionManager{
		HttpFilters: httpFilters(t),
		RouteSpecifier: &hcmv3.HttpConnectionManager_RouteConfig{RouteConfig: &routev3.RouteConfiguration{
			Name: "inbound",
			VirtualHosts: []*routev3.VirtualHost{{
				Name: "inbound", Domains: []string{"*"},
				Routes: []*routev3.Route{{
					Match:  &routev3.RouteMatch{PathSpecifier: &routev3.RouteMatch_Prefix{Prefix: ""}},
					Action: &routev3.Route_NonForwardingAction{NonForwardingAction: &routev3.NonForwardingAction{}},
				}},
			}},
		}},
	}
	return &listenerv3.Listener{
		Name:             "grpc/server?xds.resource.listening_address=" + address,
		Address:          socketAddress(t, address),
		TrafficDirection: corev3.TrafficDirection_INBOUND,
		DefaultFilterChain: &listenerv3.FilterChain{Filters: []*listenerv3.Filter{{
			Name:       "envoy.filters.network.http_connection_manager",
			ConfigType: &listenerv3.Filter_TypedConfig{TypedConfig: typed(t, hcm)},
		}}},
	}
}
