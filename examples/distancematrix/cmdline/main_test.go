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

const matrixFixture = `{"status":"OK","origin_addresses":["Harbor gate","North pier"],"destination_addresses":["Hill station","South terminal"],"rows":[{"elements":[{"status":"OK","distance":{"text":"4.3 km","value":4321},"duration":{"text":"12 mins","value":720},"duration_in_traffic":{"text":"15 mins","value":900}},{"status":"ZERO_RESULTS"}]},{"elements":[{"status":"NOT_FOUND"},{"status":"OK","distance":{"text":"7.6 km","value":7654},"duration":{"text":"20 mins","value":1200}}]}]}`

type localMatrixTransport struct {
	host string
	base http.RoundTripper
}

func (transport localMatrixTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "http" || r.URL.Host != transport.host {
		return nil, fmt.Errorf("fixture refuses non-local request: %s", r.URL)
	}
	return transport.base.RoundTrip(r)
}

func matrixServer(t *testing.T, handler http.HandlerFunc) []maps.ClientOption {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 2 * time.Second
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = localMatrixTransport{host: server.Listener.Addr().String(), base: base}
	return []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}
}

func invokeMatrix(t *testing.T, args []string, output io.Writer, options ...maps.ClientOption) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return run(ctx, args, output, &bytes.Buffer{}, options...)
}

func TestRunMatrixRequestsAndDecodedOutput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want map[string]string
	}{
		{"driving", []string{"-mode=driving", "-avoid=tolls", "-units=metric", "-traffic_model=optimistic", "-departure_time=1700000000", "-language=fr"}, map[string]string{"mode": "driving", "avoid": "tolls", "units": "metric", "traffic_model": "optimistic", "departure_time": "1700000000", "language": "fr"}},
		{"walking", []string{"-mode=walking", "-avoid=highways", "-units=imperial", "-traffic_model=best_guess"}, map[string]string{"mode": "walking", "avoid": "highways", "units": "imperial", "traffic_model": "best_guess"}},
		{"bicycling", []string{"-mode=bicycling", "-avoid=ferries", "-traffic_model=pessimistic"}, map[string]string{"mode": "bicycling", "avoid": "ferries", "traffic_model": "pessimistic"}},
		{"transit less walking", []string{"-mode=transit", "-transit_mode=bus|subway|train|tram|rail", "-transit_routing_preference=less_walking", "-arrival_time=1700000720"}, map[string]string{"mode": "transit", "transit_mode": "bus|subway|train|tram|rail", "transit_routing_preference": "less_walking", "arrival_time": "1700000720"}},
		{"transit fewer transfers", []string{"-mode=transit", "-transit_routing_preference=fewer_transfers"}, map[string]string{"mode": "transit", "transit_routing_preference": "fewer_transfers"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) {
				select {
				case requests <- r:
				default:
					t.Error("unexpected extra HTTP request")
				}
				fmt.Fprint(w, matrixFixture)
			})
			args := append([]string{"-key=offline-key", "-origins=Harbor gate|1.25,2.5", "-destinations=Hill station|place_id:terminal-42"}, tc.args...)
			var output bytes.Buffer
			code, err := invokeMatrix(t, args, &output, options...)
			if code != 0 || err != nil {
				t.Fatalf("run = (%d, %v)", code, err)
			}
			var r *http.Request
			select {
			case r = <-requests:
			default:
				t.Fatal("success did not dispatch HTTP")
			}
			if r.URL.Path != "/maps/api/distancematrix/json" {
				t.Errorf("path = %s", r.URL.Path)
			}
			want := url.Values{"key": {"offline-key"}, "origins": {"Harbor gate|1.25,2.5"}, "destinations": {"Hill station|place_id:terminal-42"}}
			for key, value := range tc.want {
				want.Set(key, value)
			}
			if r.URL.Query().Encode() != want.Encode() {
				t.Errorf("query = %v, want %v", r.URL.Query(), want)
			}
			// Literal fixtures exercise decoded rows, including per-element failures
			// which are data rather than a top-level service error.
			for _, text := range []string{"Harbor gate", "North pier", "Hill station", "South terminal", "4.3 km", "4321", "7.6 km", "7654", "ZERO_RESULTS", "NOT_FOUND"} {
				if !strings.Contains(output.String(), text) {
					t.Errorf("decoded output lacks %q: %s", text, &output)
				}
			}
		})
	}
}

func TestRunMatrixRejectsInputBeforeDispatch(t *testing.T) {
	var calls int32
	options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1); fmt.Fprint(w, matrixFixture) })
	base := []string{"-key=offline-key", "-origins=start", "-destinations=end"}
	cases := [][]string{
		{"-key=offline-key", "-destinations=end"},
		{"-key=offline-key", "-origins=start"},
		{"-origins=start", "-destinations=end"},
		{"-client_id=legacy", "-origins=start", "-destinations=end"},
		{"-signature=c2VjcmV0", "-origins=start", "-destinations=end"},
		{"-client_id=legacy", "-signature=!", "-origins=start", "-destinations=end"},
	}
	for _, suffix := range [][]string{{"-unknown"}, {"-region=fr"}, {"-mode=hovercraft"}, {"-units=nautical"}, {"-avoid=tolls|highways"}, {"-transit_mode=bus|boat"}, {"-transit_routing_preference=fastest"}, {"-traffic_model=fastest"}, {"-arrival_time=1", "-departure_time=2"}, {"-transit_mode=bus"}, {"-transit_routing_preference=less_walking"}, {"-mode=transit", "-traffic_model=best_guess"}, {"extra"}} {
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

func TestRunMatrixHelpAndIsolation(t *testing.T) {
	requests := make(chan url.Values, 3)
	options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- r.URL.Query():
		default:
			t.Error("unexpected extra HTTP request")
		}
		fmt.Fprint(w, matrixFixture)
	})
	var out, diagnostics bytes.Buffer
	code, err := run(context.Background(), []string{"-help"}, &out, &diagnostics, options...)
	if code != 0 || err != nil {
		t.Fatalf("help = (%d, %v)", code, err)
	}
	if len(requests) != 0 {
		t.Fatal("help dispatched HTTP")
	}
	first := []string{"-key=first-key", "-client_id=ignored", "-signature=!", "-origins=first|second", "-destinations=end", "-mode=transit", "-transit_mode=bus", "-units=imperial", "-avoid=ferries", "-language=fr", "-arrival_time=1700000000", "-transit_routing_preference=less_walking"}
	if code, err := invokeMatrix(t, first, &out, options...); code != 0 || err != nil {
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
	second := []string{"-client_id=legacy-client", "-signature=c2VjcmV0", "-origins=third", "-destinations=last"}
	if code, err := invokeMatrix(t, second, &out, options...); code != 0 || err != nil {
		t.Fatalf("second = (%d, %v)", code, err)
	}
	var query url.Values
	select {
	case query = <-requests:
	default:
		t.Fatal("second run did not dispatch HTTP")
	}
	if query.Get("client") != "legacy-client" || query.Get("signature") == "" || query.Get("origins") != "third" || query.Get("destinations") != "last" {
		t.Errorf("legacy request = %v", query)
	}
	for _, key := range []string{"key", "mode", "transit_mode", "units", "avoid", "language", "arrival_time", "transit_routing_preference"} {
		if _, ok := query[key]; ok {
			t.Errorf("second request leaked %s: %v", key, query)
		}
	}
	if code, _ := invokeMatrix(t, []string{"-origins=third", "-destinations=end"}, &out, options...); code != 2 {
		t.Errorf("missing credentials code = %d", code)
	}
	if code, _ := invokeMatrix(t, []string{"-key=third-key"}, &out, options...); code != 2 {
		t.Errorf("missing endpoints code = %d", code)
	}
	if len(requests) != 0 {
		t.Fatal("incomplete repeated invocation dispatched HTTP")
	}
}

func TestRunMatrixServiceFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"denied", `{"status":"REQUEST_DENIED","error_message":"fixture-matrix-denied"}`, "fixture-matrix-denied", 200},
		{"malformed", `{"status":`, "unexpected EOF", 200},
		{"HTTP", `{"error":{"message":"fixture-http-forbidden"}}`, "fixture-http-forbidden", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			var output bytes.Buffer
			code, err := invokeMatrix(t, []string{"-key=offline-key", "-origins=start", "-destinations=end"}, &output, options...)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("run = (%d, %v)", code, err)
			}
			if output.Len() != 0 || atomic.LoadInt32(&calls) != 1 {
				t.Errorf("output=%s, calls=%d", &output, calls)
			}
		})
	}
}

type failingMatrixWriter struct{ err error }

func (w failingMatrixWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunMatrixOutputFailure(t *testing.T) {
	var calls int32
	options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1); fmt.Fprint(w, matrixFixture) })
	sentinel := errors.New("fixture-output-failure")
	code, err := invokeMatrix(t, []string{"-key=offline-key", "-origins=start", "-destinations=end"}, failingMatrixWriter{sentinel}, options...)
	if code != 1 || !errors.Is(err, sentinel) {
		t.Errorf("run = (%d, %v)", code, err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("calls=%d", calls)
	}
}

func TestRunMatrixCancellation(t *testing.T) {
	var calls int32
	options := matrixServer(t, func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1); fmt.Fprint(w, matrixFixture) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	code, err := run(ctx, []string{"-key=offline-key", "-origins=start", "-destinations=end"}, &output, &bytes.Buffer{}, options...)
	if code != 1 || err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("run = (%d, %v)", code, err)
	}
	if output.Len() != 0 || atomic.LoadInt32(&calls) != 0 {
		t.Errorf("output=%s, calls=%d", &output, calls)
	}
}
