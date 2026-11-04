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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

type dispatchTransport func(*http.Request) (*http.Response, error)

func (f dispatchTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGeocodingDispatch(t *testing.T) {
	if os.Getenv("GO_GEOCODING_DISPATCH_HELPER") == "1" {
		var args []string
		if err := json.Unmarshal([]byte(os.Getenv("GO_GEOCODING_DISPATCH_ARGS")), &args); err != nil {
			t.Fatal(err)
		}
		wantQuery, err := url.ParseQuery(os.Getenv("GO_GEOCODING_DISPATCH_QUERY"))
		if err != nil {
			t.Fatal(err)
		}
		var calls int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&calls, 1)
			w.Header().Set("Content-Type", "application/json")
			if r.Method != http.MethodGet || r.URL.Path != "/maps/api/geocode/json" {
				fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"fixture-request-mismatch"}`)
				return
			}
			for _, key := range []string{"key", "address", "components", "latlng", "enable_address_descriptor"} {
				if r.URL.Query().Get(key) != wantQuery.Get(key) {
					fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"fixture-request-mismatch"}`)
					return
				}
			}
			if os.Getenv("GO_GEOCODING_DISPATCH_DENIED") == "1" {
				fmt.Fprint(w, `{"status":"REQUEST_DENIED","error_message":"fixture-geocoding-denied"}`)
				return
			}
			fmt.Fprint(w, `{
				"status":"OK",
				"results":[{"formatted_address":"fixture-formatted-address","place_id":"fixture-result-place"}],
				"address_descriptor":{
					"landmarks":[{"place_id":"fixture-landmark-place","display_name":{"text":"fixture-landmark-name","language_code":"en"}}],
					"areas":[{"place_id":"fixture-area-place","display_name":{"text":"fixture-area-name","language_code":"en"},"containment":"WITHIN"}]
				}
			}`)
		}))
		defer server.Close()
		base := http.DefaultTransport
		http.DefaultTransport = dispatchTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "maps.googleapis.com" {
				return nil, fmt.Errorf("unexpected request destination: %s", r.URL)
			}
			r.URL.Scheme = "http"
			r.URL.Host = strings.TrimPrefix(server.URL, "http://")
			return base.RoundTrip(r)
		})
		defer func() { http.DefaultTransport = base }()
		os.Args = append([]string{os.Args[0]}, args...)
		main()
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Errorf("service requests = %d, want 1", got)
		}
		return
	}

	tests := []struct {
		name       string
		address    string
		components string
		latlng     string
		reverse    bool
		denied     bool
	}{
		{name: "address-only", address: "Example Street"},
		{name: "component-only", components: "country:US"},
		{name: "coordinate-only", latlng: "12.5,-34.25", reverse: true},
		{name: "address-and-coordinates-retain-forward", address: "Example Street", latlng: "12.5,-34.25"},
		{name: "components-and-coordinates-retain-forward", components: "country:US", latlng: "12.5,-34.25"},
		{name: "forward-service-error", address: "Example Street", denied: true},
		{name: "reverse-service-error", latlng: "12.5,-34.25", reverse: true, denied: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"-key=offline-fixture-key", "-enable_address_descriptor=True"}
			query := url.Values{"key": {"offline-fixture-key"}, "enable_address_descriptor": {"true"}}
			for _, input := range []struct{ key, value string }{
				{"address", tt.address}, {"components", tt.components}, {"latlng", tt.latlng},
			} {
				if input.value != "" {
					args = append(args, "-"+input.key+"="+input.value)
					query.Set(input.key, input.value)
				}
			}
			encodedArgs, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestGeocodingDispatch$")
			cmd.Env = append(os.Environ(),
				"GO_GEOCODING_DISPATCH_HELPER=1",
				"GO_GEOCODING_DISPATCH_ARGS="+string(encodedArgs),
				"GO_GEOCODING_DISPATCH_QUERY="+query.Encode(),
			)
			if tt.denied {
				cmd.Env = append(cmd.Env, "GO_GEOCODING_DISPATCH_DENIED=1")
			}
			output, err := cmd.CombinedOutput()
			text := string(output)
			if tt.denied {
				exit, ok := err.(*exec.ExitError)
				if !ok || exit.ExitCode() != 1 || !strings.Contains(text, "fixture-geocoding-denied") {
					t.Fatalf("service error exit = %v, want status 1 and decoded API error; output: %s", err, text)
				}
				if strings.Contains(text, "fixture-formatted-address") || strings.Contains(text, "panic:") {
					t.Errorf("service failure printed success or panicked: %s", text)
				}
				return
			}
			if err != nil {
				t.Fatalf("CLI failed: %v; output: %s", err, text)
			}
			for _, value := range []string{"fixture-formatted-address", "fixture-result-place"} {
				if !strings.Contains(text, value) {
					t.Errorf("missing decoded geocoding result %q: %s", value, text)
				}
			}
			for _, value := range []string{"fixture-landmark-place", "fixture-landmark-name", "fixture-area-place", "fixture-area-name"} {
				if got := strings.Contains(text, value); got != tt.reverse {
					t.Errorf("top-level descriptor %q present = %t, want %t: %s", value, got, tt.reverse, text)
				}
			}
		})
	}
}
