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
	"regexp"
	"strings"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

const geocodingFixture = `{
	"status":"OK",
	"results":[{"formatted_address":"fixture-formatted-address","place_id":"fixture-result-place","geometry":{"location":{"lat":12.5,"lng":-34.25},"location_type":"ROOFTOP"}}],
	"address_descriptor":{
		"landmarks":[{"place_id":"fixture-landmark-place","display_name":{"text":"fixture-landmark-name","language_code":"en"}}],
		"areas":[{"place_id":"fixture-area-place","display_name":{"text":"fixture-area-name","language_code":"en"},"containment":"WITHIN"}]
	}
}`

func geocodingServer(t *testing.T, body string) (*httptest.Server, chan url.Values) {
	t.Helper()
	queries := make(chan url.Values, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/geocode/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server, queries
}

type geocodingLocalTransport func(*http.Request) (*http.Response, error)

func (f geocodingLocalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func invokeGeocoding(t *testing.T, server *httptest.Server, args []string, stdout io.Writer) (int, error, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	client := *server.Client()
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = geocodingLocalTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != server.URL {
			return nil, fmt.Errorf("offline fixture blocked destination %s", r.URL)
		}
		return base.RoundTrip(r)
	})
	code, err := run(ctx, args, stdout, &stderr, maps.WithBaseURL(server.URL), maps.WithHTTPClient(&client))
	return code, err, stderr.String()
}

func TestGeocodingDispatch(t *testing.T) {
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
		{name: "zero-coordinates", latlng: "0,0", reverse: true},
		{name: "numeric-coordinates-without-range-policy", latlng: "91,181", reverse: true},
		{name: "address-and-coordinates-retain-forward", address: "Example Street", latlng: "12.5,-34.25"},
		{name: "components-and-coordinates-retain-forward", components: "country:US", latlng: "12.5,-34.25"},
		{name: "forward-service-error", address: "Example Street", denied: true},
		{name: "reverse-service-error", latlng: "12.5,-34.25", reverse: true, denied: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := geocodingFixture
			if tt.denied {
				body = `{"status":"REQUEST_DENIED","error_message":"fixture-geocoding-denied"}`
			}
			server, queries := geocodingServer(t, body)
			args := []string{"-key=offline-fixture-key", "-enable_address_descriptor=True"}
			for _, input := range []struct{ key, value string }{{"address", tt.address}, {"components", tt.components}, {"latlng", tt.latlng}} {
				if input.value != "" {
					args = append(args, "-"+input.key+"="+input.value)
				}
			}
			var stdout bytes.Buffer
			code, err, stderr := invokeGeocoding(t, server, args, &stdout)
			if len(queries) != 1 {
				t.Fatalf("requests = %d, want 1", len(queries))
			}
			q := <-queries
			for key, want := range map[string]string{"key": "offline-fixture-key", "address": tt.address, "components": tt.components, "latlng": tt.latlng, "enable_address_descriptor": "true"} {
				if q.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, q.Get(key), want)
				}
			}
			if tt.denied {
				if code != 1 || err == nil || !strings.Contains(err.Error(), "fixture-geocoding-denied") || stdout.Len() != 0 {
					t.Fatalf("service error = (%d, %v), stdout %q", code, err, stdout.String())
				}
				return
			}
			if code != 0 || err != nil || stderr != "" {
				t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
			}
			text := stdout.String()
			for _, value := range []string{"fixture-formatted-address", "fixture-result-place", "ROOFTOP"} {
				if !strings.Contains(text, value) {
					t.Errorf("missing decoded value %q: %s", value, text)
				}
			}
			for _, pattern := range []string{`Lat:\s*12\.5`, `Lng:\s*-34\.25`} {
				if !regexp.MustCompile(pattern).MatchString(text) {
					t.Errorf("missing decoded coordinate %s: %s", pattern, text)
				}
			}
			for _, value := range []string{"fixture-landmark-place", "fixture-landmark-name", "fixture-area-place", "fixture-area-name"} {
				if got := strings.Contains(text, value); got != tt.reverse {
					t.Errorf("descriptor %q present = %t, want %t", value, got, tt.reverse)
				}
			}
		})
	}
}

type geocodingBrokenWriter struct{}

func (geocodingBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture-output-failed")
}

func TestGeocodingResponseFailures(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		broken           bool
	}{
		{"decode", `{"status":"OK","results":[`, "unexpected", false},
		{"output", geocodingFixture, "fixture-output-failed", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, queries := geocodingServer(t, tt.body)
			var buffer bytes.Buffer
			var stdout io.Writer = &buffer
			if tt.broken {
				stdout = geocodingBrokenWriter{}
			}
			code, err, _ := invokeGeocoding(t, server, []string{"-key=fixture", "-address=Example Street"}, stdout)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tt.want) || len(queries) != 1 || buffer.Len() != 0 {
				t.Fatalf("run = (%d, %v), requests %d, stdout %q", code, err, len(queries), buffer.String())
			}
		})
	}
}
