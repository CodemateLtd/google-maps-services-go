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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

type credentialsCLITransport func(*http.Request) (*http.Response, error)

func (f credentialsCLITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// Run the real CLI in a subprocess because flag parsing and usage errors exit.
func TestCredentialsCLIProcess(t *testing.T) {
	fixtureURL := os.Getenv("GO_PLACES_CREDENTIALS_FIXTURE")
	if fixtureURL == "" {
		return
	}
	target, err := url.Parse(fixtureURL)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("GO_PLACES_CREDENTIALS_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	// Route every attempted HTTP request locally, including unexpected requests.
	http.DefaultTransport = credentialsCLITransport(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		return base.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = base }()
	os.Args = append([]string{os.Args[0]}, args...)
	main()
}

func TestCredentialsCLI(t *testing.T) {
	operationArgs := []string{"-input=fixture"}
	tests := []struct {
		name  string
		args  []string
		valid bool
	}{
		{name: "API key", args: []string{"-key=offline-fixture-key"}, valid: true},
		{name: "missing credentials"},
		{name: "retired legacy pair", args: []string{"-client_id=gme-fixture", "-signature=c2VjcmV0"}},
		{name: "retired client ID only", args: []string{"-client_id=gme-fixture"}},
		{name: "retired signing secret only", args: []string{"-signature=c2VjcmV0"}},
		{name: "API key with retired client ID", args: []string{"-key=offline-fixture-key", "-client_id=gme-fixture"}},
		{name: "API key with retired signing secret", args: []string{"-key=offline-fixture-key", "-signature=c2VjcmV0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var requests []url.Values
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				mu.Lock()
				requests = append(requests, query)
				mu.Unlock()
				if r.Method != http.MethodGet || r.URL.Path != "/maps/api/place/autocomplete/json" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]interface{}{"status": "OK"}); err != nil {
					t.Errorf("writing Places fixture: %v", err)
				}
			}))
			defer server.Close()
			args := append(append([]string(nil), operationArgs...), tt.args...)
			encodedArgs, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCredentialsCLIProcess$")
			cmd.Env = append(os.Environ(),
				"GO_PLACES_CREDENTIALS_FIXTURE="+server.URL,
				"GO_PLACES_CREDENTIALS_ARGS="+string(encodedArgs),
			)
			output, err := cmd.CombinedOutput()
			mu.Lock()
			gotRequests := append([]url.Values(nil), requests...)
			mu.Unlock()
			if ctx.Err() != nil {
				t.Fatalf("CLI timed out: %v; output: %s", ctx.Err(), output)
			}
			if !tt.valid {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
					t.Errorf("invalid credentials exit = %v, want usage exit 2; output: %s", err, output)
				}
				if len(gotRequests) != 0 {
					t.Errorf("invalid credentials reached HTTP: %#v", gotRequests)
				}
				return
			}
			if err != nil {
				t.Fatalf("API-key CLI failed: %v; output: %s", err, output)
			}
			if len(gotRequests) != 1 {
				t.Fatalf("HTTP requests = %d, want 1", len(gotRequests))
			}
			query := gotRequests[0]
			if keys := query["key"]; len(keys) != 1 || keys[0] != "offline-fixture-key" {
				t.Errorf("SDK API-key query = %#v, want [offline-fixture-key]", keys)
			}
			for _, retired := range []string{"client", "signature"} {
				if values, present := query[retired]; present {
					t.Errorf("API-key request sent %s = %#v", retired, values)
				}
			}
		})
	}
}
