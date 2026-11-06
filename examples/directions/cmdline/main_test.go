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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

type iterationTransport func(*http.Request) (*http.Response, error)

func (f iterationTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestConcurrentIterationErrors(t *testing.T) {
	if os.Getenv("GO_DIRECTIONS_ITERATION_HELPER") == "1" {
		var calls int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if atomic.AddInt32(&calls, 1) == 1 {
				fmt.Fprint(w, `{"status":"OK","routes":[],"geocoded_waypoints":[]}`)
			} else {
				fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"fixture-directions-denied"}`)
			}
		}))
		defer server.Close()
		base := http.DefaultTransport
		http.DefaultTransport = iterationTransport(func(r *http.Request) (*http.Response, error) {
			r.URL.Scheme = "http"
			r.URL.Host = strings.TrimPrefix(server.URL, "http://")
			return base.RoundTrip(r)
		})
		defer func() { http.DefaultTransport = base }()
		os.Args = []string{os.Args[0], "-key=offline-fixture-key", "-origin=start", "-destination=end", "-iterations=2"}
		main()
		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Fatalf("request count = %d, want 2", got)
		}
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestConcurrentIterationErrors$")
	cmd.Env = append(os.Environ(), "GO_DIRECTIONS_ITERATION_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Directions example failed: %v; output: %s", err, output)
	}
	var successes, failures int
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Iteration ") {
			successes++
		} else if strings.HasPrefix(line, "error: ") {
			failures++
		}
	}
	if successes != 1 || failures != 1 || strings.Count(string(output), "fixture-directions-denied") != 1 {
		t.Errorf("want one success and one actual request error, in either order; got: %s", output)
	}
}
