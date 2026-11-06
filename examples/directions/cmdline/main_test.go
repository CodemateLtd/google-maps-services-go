// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

const directionsFixture = `{"status":"OK","geocoded_waypoints":[{"geocoder_status":"OK","place_id":"fixture-waypoint-42","types":["street_address"]}],"routes":[{"summary":"Harbor route","waypoint_order":[1,0],"legs":[{"start_address":"Harbor gate","end_address":"Hill station","distance":{"text":"4.3 km","value":4321},"duration":{"text":"12 mins","value":720}}]}]}`

type localDirectionsTransport struct {
	host string
	base http.RoundTripper
}

func (transport localDirectionsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != transport.host {
		return nil, fmt.Errorf("fixture refuses non-local request: %s", r.URL)
	}
	return transport.base.RoundTrip(r)
}

func directionsServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, []maps.ClientOption) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 2 * time.Second
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = localDirectionsTransport{host: server.Listener.Addr().String(), base: base}
	return server, []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}
}

func invokeDirections(t *testing.T, args []string, out io.Writer, options ...maps.ClientOption) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return run(ctx, args, out, &bytes.Buffer{}, options...)
}

func TestRunDirectionsRequestsAndDecodedOutput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"driving", []string{"-mode=driving", "-units=metric", "-traffic_model=optimistic", "-departure_time=1700000000", "-waypoints=via:Harbor|1.25,2.5", "-avoid=tolls|highways|ferries", "-alternatives", "-language=fr", "-region=fr"}, map[string]string{"mode": "driving", "units": "metric", "traffic_model": "optimistic", "departure_time": "1700000000", "waypoints": "via:Harbor|1.25,2.5", "avoid": "tolls|highways|ferries", "alternatives": "true", "language": "fr", "region": "fr"}},
		{"walking", []string{"-mode=walking", "-units=imperial", "-traffic_model=best_guess"}, map[string]string{"mode": "walking", "units": "imperial", "traffic_model": "best_guess"}},
		{"bicycling", []string{"-mode=bicycling", "-traffic_model=pessimistic"}, map[string]string{"mode": "bicycling", "traffic_model": "pessimistic"}},
		{"transit less walking", []string{"-mode=transit", "-transit_mode=bus|subway|train|tram|rail", "-transit_routing_preference=less_walking", "-arrival_time=1700000720"}, map[string]string{"mode": "transit", "transit_mode": "bus|subway|train|tram|rail", "transit_routing_preference": "less_walking", "arrival_time": "1700000720"}},
		{"transit fewer transfers", []string{"-mode=transit", "-transit_routing_preference=fewer_transfers"}, map[string]string{"mode": "transit", "transit_routing_preference": "fewer_transfers"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- r:
				default:
					t.Error("unexpected extra HTTP request")
				}
				fmt.Fprint(w, directionsFixture)
			})
			args := append([]string{"-key=offline-key", "-origin=Harbor gate", "-destination=Hill station"}, tc.args...)
			var output bytes.Buffer
			code, err := invokeDirections(t, args, &output, options...)
			if code != 0 || err != nil {
				t.Fatalf("run = (%d, %v)", code, err)
			}
			var r *http.Request
			select {
			case r = <-requests:
			default:
				t.Fatal("success did not dispatch HTTP")
			}
			if r.URL.Path != "/maps/api/directions/json" {
				t.Errorf("path = %s", r.URL.Path)
			}
			want := url.Values{"key": {"offline-key"}, "origin": {"Harbor gate"}, "destination": {"Hill station"}}
			for key, value := range tc.want {
				want.Set(key, value)
			}
			if r.URL.Query().Encode() != want.Encode() {
				t.Errorf("query = %v; want %v", r.URL.Query(), want)
			}
			for _, text := range []string{"fixture-waypoint-42", "Harbor route", "Harbor gate", "Hill station", "4.3 km", "4321"} {
				if !strings.Contains(output.String(), text) {
					t.Errorf("decoded output lacks %q: %s", text, &output)
				}
			}
		})
	}
}

func TestRunDirectionsRejectsInputBeforeDispatch(t *testing.T) {
	var calls int32
	_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, directionsFixture)
	})
	base := []string{"-key=offline-key", "-origin=start", "-destination=end"}
	cases := [][]string{
		{"-key=offline-key", "-destination=end"},
		{"-key=offline-key", "-origin=start"},
		{"-origin=start", "-destination=end"},
		{"-client_id=legacy", "-origin=start", "-destination=end"},
		{"-signature=c2VjcmV0", "-origin=start", "-destination=end"},
		{"-client_id=legacy", "-signature=!", "-origin=start", "-destination=end"},
	}
	for _, suffix := range [][]string{{"-unknown"}, {"-alternatives=maybe"}, {"-iterations=bad"}, {"-iterations=0"}, {"-iterations=-1"}, {"-mode=hovercraft"}, {"-units=nautical"}, {"-avoid=tolls|tunnels"}, {"-transit_mode=bus|boat"}, {"-transit_routing_preference=fastest"}, {"-traffic_model=fastest"}, {"-arrival_time=1", "-departure_time=2"}, {"-transit_mode=bus"}, {"-transit_routing_preference=less_walking"}, {"extra"}} {
		cases = append(cases, append(append([]string{}, base...), suffix...))
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			code, err := run(ctx, args, &out, &diagnostics, options...)
			if code != 2 || (err == nil && diagnostics.Len() == 0) {
				t.Errorf("run = (%d, %v), diagnostics=%s", code, err, &diagnostics)
			}
			if out.Len() != 0 {
				t.Errorf("unexpected success output: %s", &out)
			}
		})
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("invalid input dispatched %d requests", got)
	}
}

func TestRunDirectionsHelpAndIsolation(t *testing.T) {
	requests := make(chan url.Values, 3)
	_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- r.URL.Query():
		default:
			t.Error("unexpected extra HTTP request")
		}
		fmt.Fprint(w, directionsFixture)
	})
	var out, diagnostics bytes.Buffer
	code, err := run(context.Background(), []string{"-help"}, &out, &diagnostics, options...)
	if code != 0 || err != nil {
		t.Fatalf("help = (%d, %v)", code, err)
	}
	if len(requests) != 0 {
		t.Fatal("help dispatched HTTP")
	}
	// The key takes precedence even over malformed legacy credentials.
	first := []string{"-key=first-key", "-client_id=ignored", "-signature=!", "-origin=first", "-destination=end", "-mode=transit", "-transit_mode=bus", "-alternatives", "-waypoints=middle"}
	if code, err := invokeDirections(t, first, &out, options...); code != 0 || err != nil {
		t.Fatalf("first = (%d, %v)", code, err)
	}
	select {
	case query := <-requests:
		if query.Get("key") != "first-key" || query.Get("client") != "" {
			t.Errorf("key precedence request = %v", query)
		}
	default:
		t.Fatal("first run did not dispatch HTTP")
	}
	second := []string{"-client_id=legacy-client", "-signature=c2VjcmV0", "-origin=second", "-destination=end"}
	if code, err := invokeDirections(t, second, &out, options...); code != 0 || err != nil {
		t.Fatalf("second = (%d, %v)", code, err)
	}
	var query url.Values
	select {
	case query = <-requests:
	default:
		t.Fatal("second run did not dispatch HTTP")
	}
	if query.Get("client") != "legacy-client" || query.Get("signature") == "" || query.Get("origin") != "second" {
		t.Errorf("legacy request = %v", query)
	}
	for _, key := range []string{"key", "mode", "transit_mode", "alternatives", "waypoints"} {
		if _, ok := query[key]; ok {
			t.Errorf("second request leaked %s: %v", key, query)
		}
	}
	// Neither credentials nor required arguments survive a prior invocation.
	if code, _ := invokeDirections(t, []string{"-origin=third", "-destination=end"}, &out, options...); code != 2 {
		t.Errorf("missing credentials code = %d", code)
	}
	if code, _ := invokeDirections(t, []string{"-key=third-key"}, &out, options...); code != 2 {
		t.Errorf("missing endpoints code = %d", code)
	}
	if len(requests) != 0 {
		t.Fatal("incomplete repeated invocation dispatched HTTP")
	}
}

func TestRunDirectionsServiceFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"denied", `{"status":"REQUEST_DENIED","error_message":"fixture-directions-denied"}`, "fixture-directions-denied", 200},
		{"malformed", `{"status":`, "unexpected EOF", 200},
		{"HTTP", `{"error":{"message":"fixture-http-forbidden"}}`, "fixture-http-forbidden", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			var output bytes.Buffer
			code, err := invokeDirections(t, []string{"-key=offline-key", "-origin=start", "-destination=end"}, &output, options...)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("run = (%d, %v)", code, err)
			}
			if output.Len() != 0 || atomic.LoadInt32(&calls) != 1 {
				t.Errorf("output=%s, calls=%d", &output, calls)
			}
		})
	}
}

// This is the GO10 regression: one failed concurrent call must not hide the
// other outcome or cause an unsuccessful process exit. Order is unspecified.
func TestConcurrentIterationErrors(t *testing.T) {
	var calls int32
	_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			fmt.Fprint(w, directionsFixture)
		} else {
			fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"fixture-directions-denied"}`)
		}
	})
	var output bytes.Buffer
	code, err := invokeDirections(t, []string{"-key=offline-key", "-origin=start", "-destination=end", "-iterations=2"}, &output, options...)
	if code != 0 || err == nil || !strings.Contains(err.Error(), "fixture-directions-denied") {
		t.Fatalf("batch = (%d, %v)", code, err)
	}
	assertIterationLines(t, output.String(), 1, 1)
	if atomic.LoadInt32(&calls) != 2 || strings.Count(output.String(), "fixture-directions-denied") != 1 {
		t.Errorf("calls=%d, output=%s", calls, &output)
	}
}

func assertIterationLines(t *testing.T, output string, wantSuccess, wantFailure int) {
	t.Helper()
	var successes, failures int
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.HasPrefix(line, "Iteration ") && strings.Contains(line, ": round trip ") {
			successes++
		} else if strings.HasPrefix(line, "error: ") {
			failures++
		} else {
			t.Errorf("unexpected batch output line %q", line)
		}
	}
	if successes != wantSuccess || failures != wantFailure {
		t.Errorf("batch successes/failures = %d/%d, want %d/%d: %s", successes, failures, wantSuccess, wantFailure, output)
	}
}

func TestRunDirectionsHomogeneousBatches(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("failure=", fail), func(t *testing.T) {
			var calls int32
			_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				if fail {
					fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"batch-denied"}`)
				} else {
					fmt.Fprint(w, directionsFixture)
				}
			})
			var output bytes.Buffer
			code, err := invokeDirections(t, []string{"-key=offline-key", "-origin=start", "-destination=end", "-iterations=3"}, &output, options...)
			if code != 0 || (err != nil) != fail {
				t.Fatalf("batch = (%d, %v)", code, err)
			}
			if fail {
				assertIterationLines(t, output.String(), 0, 3)
				if strings.Count(output.String(), "batch-denied") != 3 {
					t.Errorf("lost errors: %s", &output)
				}
			} else {
				assertIterationLines(t, output.String(), 3, 0)
			}
			if atomic.LoadInt32(&calls) != 3 {
				t.Errorf("calls=%d", calls)
			}
		})
	}
}

type failingDirectionsWriter struct{ err error }

func (w failingDirectionsWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunDirectionsOutputFailureDrainsBatch(t *testing.T) {
	for _, iterations := range []string{"1", "3"} {
		t.Run(iterations, func(t *testing.T) {
			var calls int32
			_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				fmt.Fprint(w, directionsFixture)
			})
			sentinel := errors.New("fixture-output-failure")
			code, err := invokeDirections(t, []string{"-key=offline-key", "-origin=start", "-destination=end", "-iterations=" + iterations}, failingDirectionsWriter{sentinel}, options...)
			if code != 1 || !errors.Is(err, sentinel) {
				t.Errorf("run = (%d, %v)", code, err)
			}
			want := int32(1)
			if iterations == "3" {
				want = 3
			}
			if atomic.LoadInt32(&calls) != want {
				t.Errorf("calls=%d, want %d", calls, want)
			}
		})
	}
}

func TestRunDirectionsCancellation(t *testing.T) {
	var calls int32
	_, options := directionsServer(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, directionsFixture)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	code, err := run(ctx, []string{"-key=offline-key", "-origin=start", "-destination=end", "-iterations=3"}, &output, &bytes.Buffer{}, options...)
	if code != 0 || err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("run = (%d, %v)", code, err)
	}
	assertIterationLines(t, output.String(), 0, 3)
	if atomic.LoadInt32(&calls) != 0 {
		t.Errorf("canceled batch dispatched %d requests", calls)
	}
}
