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
	"fmt"
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

type componentsCLITransport func(*http.Request) (*http.Response, error)

func (f componentsCLITransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestPlaceAutocompleteComponentsProcess(t *testing.T) {
	fixtureURL := os.Getenv("GO_AUTOCOMPLETE_COMPONENTS_FIXTURE")
	if fixtureURL == "" {
		return
	}
	target, err := url.Parse(fixtureURL)
	if err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	http.DefaultTransport = componentsCLITransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "maps.googleapis.com" || r.URL.Path != "/maps/api/place/autocomplete/json" {
			return nil, fmt.Errorf("unexpected autocomplete request: %s", r.URL)
		}
		r.URL.Scheme = target.Scheme
		r.URL.Host = target.Host
		return base.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = base }()
	os.Args = []string{os.Args[0], "-key=offline-fixture-key", "-input=Springfield"}
	if os.Getenv("GO_AUTOCOMPLETE_COMPONENTS_FLAG") == "1" {
		os.Args = append(os.Args, "-components="+os.Getenv("GO_AUTOCOMPLETE_COMPONENTS_VALUE"))
	}
	main()
}

func TestPlaceAutocompleteComponentsCLI(t *testing.T) {
	places := []struct {
		country     string
		description string
	}{
		{"us", "FixtureSpringfieldUS"},
		{"ca", "FixtureSpringfieldCA"},
		{"gb", "FixtureSpringfieldGB"},
	}
	tests := []struct {
		name          string
		components    string
		includeFlag   bool
		wantCountries string
		invalid       bool
	}{
		{name: "single country", components: "country:us", includeFlag: true, wantCountries: "us"},
		{name: "repeated countries", components: "country:us|country:ca", includeFlag: true, wantCountries: "us,ca"},
		{name: "omitted", wantCountries: "us,ca,gb"},
		{name: "explicit empty", includeFlag: true, wantCountries: "us,ca,gb"},
		{name: "missing colon", components: "country", includeFlag: true, invalid: true},
		{name: "empty country", components: "country:", includeFlag: true, invalid: true},
		{name: "extra colon", components: "country:us:ca", includeFlag: true, invalid: true},
		{name: "unknown component", components: "locality:Springfield", includeFlag: true, invalid: true},
		{name: "empty later token", components: "country:us|", includeFlag: true, invalid: true},
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
				if r.Method != http.MethodGet || r.URL.Path != "/maps/api/place/autocomplete/json" || query.Get("input") != "Springfield" || query.Get("key") != "offline-fixture-key" {
					t.Errorf("unexpected autocomplete request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected request", http.StatusBadRequest)
					return
				}
				// Model country filtering over a fixed cross-country result set,
				// rather than returning the query or an injected SDK request.
				allowed := make(map[string]bool)
				if components := query.Get("components"); components != "" {
					for _, token := range strings.Split(components, "|") {
						allowed[strings.TrimPrefix(token, "country:")] = true
					}
				}
				predictions := make([]map[string]string, 0, len(places))
				for _, place := range places {
					if len(allowed) == 0 || allowed[place.country] {
						predictions = append(predictions, map[string]string{"description": place.description})
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]interface{}{"status": "OK", "predictions": predictions}); err != nil {
					t.Errorf("writing autocomplete fixture: %v", err)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPlaceAutocompleteComponentsProcess$")
			includeFlag := "0"
			if tt.includeFlag {
				includeFlag = "1"
			}
			cmd.Env = append(os.Environ(),
				"GO_AUTOCOMPLETE_COMPONENTS_FIXTURE="+server.URL,
				"GO_AUTOCOMPLETE_COMPONENTS_FLAG="+includeFlag,
				"GO_AUTOCOMPLETE_COMPONENTS_VALUE="+tt.components,
			)
			output, err := cmd.CombinedOutput()
			mu.Lock()
			gotRequests := append([]url.Values(nil), requests...)
			mu.Unlock()
			if tt.invalid {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() == 0 {
					t.Fatalf("invalid components did not fail in the CLI: %v; output: %s", err, output)
				}
				if len(gotRequests) != 0 {
					t.Errorf("invalid components reached HTTP: %#v", gotRequests)
				}
				if !strings.Contains(string(output), "component") {
					t.Errorf("failure did not identify a component error: %s", output)
				}
				return
			}
			if err != nil {
				t.Fatalf("autocomplete CLI failed: %v; output: %s", err, output)
			}
			if len(gotRequests) != 1 {
				t.Fatalf("HTTP requests = %d, want 1", len(gotRequests))
			}
			if tt.components == "" {
				if _, present := gotRequests[0]["components"]; present {
					t.Errorf("omitted components sent a query parameter: %#v", gotRequests[0])
				}
			} else if got := gotRequests[0]["components"]; len(got) != 1 || got[0] != tt.components {
				t.Errorf("components query = %#v, want [%s]", got, tt.components)
			}
			for _, place := range places {
				wantCount := 0
				for _, country := range strings.Split(tt.wantCountries, ",") {
					if country == place.country {
						wantCount = 1
					}
				}
				if got := strings.Count(string(output), place.description); got != wantCount {
					t.Errorf("prediction %s appears %d times, want %d; output: %s", place.description, got, wantCount, output)
				}
			}
		})
	}
}
