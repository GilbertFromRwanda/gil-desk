// Command loadtest drives concurrent simulated devices against the real
// running backend's gRPC RendezvousService (planner task G-28, "load
// test 10k connections"). "Connections" here means concurrent device
// sessions (RegisterDevice + repeated Heartbeat calls) multiplexed over
// a single grpc.ClientConn — the realistic shape of load this backend
// actually needs to hold up under, not 10k raw TCP sockets (gRPC's own
// HTTP/2 multiplexing means those two things aren't the same, and 10k
// simultaneous real sockets from one process would just be testing this
// machine's OS limits, not the backend).
//
// Usage (against a real running backend):
//
//	go run ./cmd/loadtest -devices 10000 -heartbeats 3
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	nexdeskv1 "github.com/nexdesk/nexdesk/backend/gen/nexdesk/v1"
)

type rpcResult struct {
	rpc string
	dur time.Duration
	err error
}

func main() {
	devices := flag.Int("devices", 2000, "number of concurrent simulated devices")
	heartbeats := flag.Int("heartbeats", 3, "heartbeat calls per device after registration")
	// 127.0.0.1, not "localhost": resolving "localhost" added several
	// *seconds* of latency to every single RPC in early runs of this
	// tool on Windows (it tries the IPv6 ::1 result first, times out,
	// then falls back to IPv4) — a client-side DNS quirk that looked
	// exactly like a server-side bottleneck until isolated. Real finding
	// from actually running this load test, not a hypothetical one.
	httpAddr := flag.String("http", "http://127.0.0.1:8080", "backend HTTP base URL (for registering the one test account)")
	grpcAddr := flag.String("grpc", "127.0.0.1:9090", "backend gRPC address")
	flag.Parse()

	token, err := registerAndLogin(*httpAddr)
	if err != nil {
		fmt.Println("setup failed:", err)
		return
	}

	// One shared ClientConn, not one per simulated device — gRPC
	// multiplexes concurrent RPCs over it via HTTP/2, which is how a
	// real gRPC client is meant to be used, and is exactly what makes
	// "10k concurrent connections" meaningful to test from one process.
	conn, err := grpc.NewClient(*grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Println("dial failed:", err)
		return
	}
	defer conn.Close()
	client := nexdeskv1.NewRendezvousServiceClient(conn)

	results := make(chan rpcResult, *devices*(*heartbeats+1))
	var wg sync.WaitGroup
	start := time.Now()
	runID := start.UnixNano()

	for i := 0; i < *devices; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			deviceID := fmt.Sprintf("loadtest-device-%d-%d", runID, i)
			ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)

			t0 := time.Now()
			_, err := client.RegisterDevice(ctx, &nexdeskv1.RegisterDeviceRequest{
				DeviceId:  deviceID,
				PublicKey: "loadtest-pubkey",
			})
			results <- rpcResult{"RegisterDevice", time.Since(t0), err}
			if err != nil {
				return
			}

			for h := 0; h < *heartbeats; h++ {
				t1 := time.Now()
				_, err := client.Heartbeat(ctx, &nexdeskv1.HeartbeatRequest{
					DeviceId: deviceID,
					Endpoint: "127.0.0.1:0",
				})
				results <- rpcResult{"Heartbeat", time.Since(t1), err}
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	durations := map[string][]time.Duration{}
	errCounts := map[string]int{}
	sampleErrors := map[string]error{}
	total := 0
	for r := range results {
		total++
		if r.err != nil {
			errCounts[r.rpc]++
			sampleErrors[r.rpc] = r.err
			continue
		}
		durations[r.rpc] = append(durations[r.rpc], r.dur)
	}
	elapsed := time.Since(start)

	fmt.Printf("=== loadtest: %d devices x (1 register + %d heartbeats) = %d RPCs in %s (%.0f rps) ===\n",
		*devices, *heartbeats, total, elapsed, float64(total)/elapsed.Seconds())
	for _, rpc := range []string{"RegisterDevice", "Heartbeat"} {
		durs := durations[rpc]
		errs := errCounts[rpc]
		if len(durs) == 0 {
			fmt.Printf("%-16s n=0 errors=%d", rpc, errs)
			if e, ok := sampleErrors[rpc]; ok {
				fmt.Printf(" (e.g. %v)", e)
			}
			fmt.Println()
			continue
		}
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		p50 := durs[len(durs)*50/100]
		p95 := durs[len(durs)*95/100]
		p99 := durs[min(len(durs)*99/100, len(durs)-1)]
		max := durs[len(durs)-1]
		fmt.Printf("%-16s n=%-6d errors=%-5d p50=%-10s p95=%-10s p99=%-10s max=%s\n",
			rpc, len(durs), errs, p50, p95, p99, max)
	}
}

func registerAndLogin(httpAddr string) (string, error) {
	email := fmt.Sprintf("loadtest-%d@example.com", time.Now().UnixNano())
	body, _ := json.Marshal(map[string]string{"email": email, "password": "correcthorsebattery"})
	resp, err := http.Post(httpAddr+"/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register: status %d: %s", resp.StatusCode, respBody)
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("register: parse response: %w", err)
	}
	return parsed.AccessToken, nil
}
