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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// Run the real command in a child process because usage and request errors exit.
// Every HTTP request is routed to the local fixture, including unexpected ones.
func TestNearestRoadsCLIProcess(t *testing.T) {
	fixtureURL := os.Getenv("GO_NEAREST_ROADS_FIXTURE")
	if fixtureURL == "" {
		return
	}
	target, err := url.Parse(fixtureURL)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("GO_NEAREST_ROADS_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	http.DefaultTransport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
		request.URL.Scheme = target.Scheme
		request.URL.Host = target.Host
		return base.RoundTrip(request)
	})
	os.Args = append([]string{os.Args[0]}, args...)
	main()
	os.Exit(0)
}

func TestNearestRoadsCLI(t *testing.T) {
	const key = "offline-nearest-roads-key"
	const independentPoints = "-35.27801,149.12958|40.714,-74.006|51.5074,-0.1278"
	// The static fixture is deliberately not a request echo: returned coordinates
	// differ, mappings are out of order and repeated, and one index is absent.
	const snappedPoints = `{"snappedPoints":[
		{"location":{"latitude":51.5075,"longitude":-0.1279},"originalIndex":2,"placeId":"road-london-a"},
		{"location":{"latitude":-35.2784,"longitude":149.1294},"originalIndex":0,"placeId":"road-canberra"},
		{"location":{"latitude":51.5076,"longitude":-0.128},"originalIndex":2,"placeId":"road-london-b"},
		{"location":{"latitude":40.7141,"longitude":-74.0061},"placeId":"road-without-index"}
	]}`
	const formattedPoints = "location=51.5075,-0.1279 place_id=\"road-london-a\" original_index=2\n" +
		"location=-35.2784,149.1294 place_id=\"road-canberra\" original_index=0\n" +
		"location=51.5076,-0.128 place_id=\"road-london-b\" original_index=2\n" +
		"location=40.7141,-74.0061 place_id=\"road-without-index\" original_index=absent\n"
	type testCase struct {
		name       string
		args       []string
		disconnect bool
		response   string
		wantPoints string
		wantOutput string
		wantError  string
		wantExit   int
		help       bool
	}
	tests := []testCase{
		{
			name:     "independent points and original index mappings",
			args:     []string{"-key=" + key, "-points=" + independentPoints},
			response: snappedPoints, wantPoints: independentPoints, wantOutput: formattedPoints,
		},
		{
			name:     "inclusive coordinate boundaries",
			args:     []string{"-key=" + key, "-points=90,180|-90,-180"},
			response: `{"snappedPoints":[]}`, wantPoints: "90,180|-90,-180",
		},
		{
			name:       "connection closed before response",
			args:       []string{"-key=" + key, "-points=" + independentPoints},
			disconnect: true, wantPoints: independentPoints,
			wantError: "Nearest Roads failed: EOF", wantExit: 1,
		},
		{
			name:     "invalid service JSON",
			args:     []string{"-key=" + key, "-points=" + independentPoints},
			response: `{"snappedPoints":`, wantPoints: independentPoints,
			wantError: "Nearest Roads failed: unexpected EOF", wantExit: 1,
		},
		{
			name:     "invalid decoded index",
			args:     []string{"-key=" + key, "-points=" + independentPoints},
			response: `{"snappedPoints":[{"originalIndex":"not-an-index"}]}`, wantPoints: independentPoints,
			wantError: "Nearest Roads failed: json: cannot unmarshal string", wantExit: 1,
		},
		{name: "missing key", args: []string{"-points=" + independentPoints}, wantError: "Please specify an API key", wantExit: 2},
		{name: "missing points", args: []string{"-key=" + key}, wantError: "Please specify independent points", wantExit: 2},
		{name: "whitespace points", args: []string{"-key=" + key, "-points= "}, wantError: "Please specify independent points", wantExit: 2},
		{name: "help", args: []string{"-h"}, help: true},
		{name: "unexpected positional argument", args: []string{"-key=" + key, "-points=" + independentPoints, "extra"}, wantError: "Unexpected positional arguments", wantExit: 2},
	}
	for _, invalid := range []struct {
		name   string
		points string
		error  string
	}{
		{name: "missing comma", points: "35", error: "Point 0 must be a latitude,longitude pair"},
		{name: "extra coordinate", points: "35,149,1", error: "Point 0 must be a latitude,longitude pair"},
		{name: "missing latitude", points: ",149", error: "Point 0 must contain two numeric coordinates"},
		{name: "missing longitude", points: "35,", error: "Point 0 must contain two numeric coordinates"},
		{name: "empty pair", points: "35,149||36,150", error: "Point 1 must be a latitude,longitude pair"},
		{name: "trailing separator", points: "35,149|", error: "Point 1 must be a latitude,longitude pair"},
		{name: "nonnumeric coordinate", points: "north,149", error: "Point 0 must contain two numeric coordinates"},
		{name: "NaN latitude", points: "NaN,149", error: "coordinates must be finite"},
		{name: "NaN longitude", points: "35,NaN", error: "coordinates must be finite"},
		{name: "infinite latitude", points: "+Inf,149", error: "coordinates must be finite"},
		{name: "infinite longitude", points: "35,-Inf", error: "coordinates must be finite"},
		{name: "overflow", points: "1e1000,149", error: "must contain two numeric coordinates"},
		{name: "latitude too high", points: "90.01,149", error: "latitude must be in [-90,90]"},
		{name: "latitude too low", points: "-90.01,149", error: "latitude must be in [-90,90]"},
		{name: "longitude too high", points: "35,180.01", error: "longitude in [-180,180]"},
		{name: "longitude too low", points: "35,-180.01", error: "longitude in [-180,180]"},
	} {
		tests = append(tests, testCase{
			name: invalid.name, args: []string{"-key=" + key, "-points=" + invalid.points},
			wantError: invalid.error, wantExit: 2,
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.Query())
				mu.Unlock()
				if r.Method != http.MethodGet || r.URL.Path != "/v1/nearestRoads" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if tt.disconnect {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("hijacking fixture connection: %v", err)
						return
					}
					connection.Close()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := w.Write([]byte(tt.response)); err != nil {
					t.Errorf("writing fixture: %v", err)
				}
			}))
			defer server.Close()
			encodedArgs, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNearestRoadsCLIProcess$")
			cmd.Env = append(os.Environ(), "GO_NEAREST_ROADS_FIXTURE="+server.URL, "GO_NEAREST_ROADS_ARGS="+string(encodedArgs))
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			if ctx.Err() != nil {
				t.Fatalf("CLI timed out: %v; stderr: %s", ctx.Err(), &stderr)
			}
			gotExit := 0
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					gotExit = exit.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			if gotExit != tt.wantExit {
				t.Errorf("exit = %d, want %d; stderr: %s", gotExit, tt.wantExit, &stderr)
			}
			if stdout.String() != tt.wantOutput {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantOutput)
			}
			if !strings.Contains(stderr.String(), tt.wantError) {
				t.Errorf("stderr = %q, want error containing %q", stderr.String(), tt.wantError)
			}
			if tt.wantExit == 0 && !tt.help && stderr.Len() != 0 {
				t.Errorf("unexpected stderr: %s", &stderr)
			}
			if strings.Contains(stdout.String()+stderr.String(), key) {
				t.Error("CLI disclosed the API key")
			}
			mu.Lock()
			gotRequests := append([]url.Values(nil), requests...)
			mu.Unlock()
			if tt.wantPoints == "" {
				if len(gotRequests) != 0 {
					t.Errorf("invalid input/help reached HTTP: %#v", gotRequests)
				}
				return
			}
			if len(gotRequests) != 1 {
				t.Fatalf("HTTP requests = %d, want 1", len(gotRequests))
			}
			query := gotRequests[0]
			if got := query["points"]; len(got) != 1 || got[0] != tt.wantPoints {
				t.Errorf("points = %#v, want [%s] in input order", got, tt.wantPoints)
			}
			if got := query["key"]; len(got) != 1 || got[0] != key {
				t.Errorf("key = %#v, want fixture API key", got)
			}
			for _, forbidden := range []string{"path", "interpolate", "client", "signature"} {
				if _, present := query[forbidden]; present {
					t.Errorf("Nearest Roads unexpectedly sent %s", forbidden)
				}
			}
		})
	}
}
