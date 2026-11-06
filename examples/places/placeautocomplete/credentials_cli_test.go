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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

type offlineTransport struct {
	host string
	base http.RoundTripper
}

func (tr offlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != tr.host {
		return nil, fmt.Errorf("non-fixture HTTP request: %s", r.URL)
	}
	return tr.base.RoundTrip(r)
}

func fixture(t *testing.T, status int, contentType string, body []byte) ([]maps.ClientOption, chan url.Values) {
	t.Helper()
	requests := make(chan url.Values, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/place/autocomplete/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		w.Write(body)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Transport = offlineTransport{host: target.Host, base: client.Transport}
	client.Timeout = 5 * time.Second
	return []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}, requests
}

func invoke(t *testing.T, args []string, stdout, stderr io.Writer, options []maps.ClientOption) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return run(ctx, args, stdout, stderr, options...)
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
			options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK"}`))
			args := append(append([]string(nil), operationArgs...), tt.args...)
			var stdout, stderr bytes.Buffer
			code, err := invoke(t, args, &stdout, &stderr, options)
			if !tt.valid {
				if code != 2 {
					t.Errorf("invalid credentials code = %d, error = %v", code, err)
				}
				if len(requests) != 0 {
					t.Errorf("invalid credentials made %d HTTP calls", len(requests))
				}
				return
			}
			if code != 0 || err != nil {
				t.Fatalf("run = (%d, %v)", code, err)
			}
			if len(requests) != 1 {
				t.Fatalf("HTTP requests = %d, want 1", len(requests))
			}
			query := <-requests
			if keys := query["key"]; len(keys) != 1 || keys[0] != "offline-fixture-key" {
				t.Errorf("API-key query = %#v", keys)
			}
			for _, retired := range []string{"client", "signature"} {
				if values, present := query[retired]; present {
					t.Errorf("request sent %s = %#v", retired, values)
				}
			}
		})
	}
}
