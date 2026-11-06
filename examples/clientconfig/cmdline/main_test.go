// Copyright 2026 Google Inc. All Rights Reserved.
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
	"testing"
	"time"
)

func testOptions() options {
	return options{
		key:            "fictional-sensitive-api-key",
		address:        "A requested address",
		httpTimeout:    5 * time.Second,
		requestTimeout: 3 * time.Second,
	}
}

func TestLocalCommandAndHelp(t *testing.T) {
	var out, diagnostics bytes.Buffer
	if code := run(context.Background(), []string{"-local"}, &out, &diagnostics); code != 0 {
		t.Fatalf("local exit = %d: %s", code, diagnostics.String())
	}
	if got, want := out.String(), "Fictional fixture address: 1.250000,2.500000\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if got, want := diagnostics.String(), "metric path=/maps/api/geocode/json status=200 error=false\n"; got != want {
		t.Fatalf("metrics = %q, want %q", got, want)
	}
	out.Reset()
	diagnostics.Reset()
	if code := run(context.Background(), []string{"-key", "not-to-be-logged", "-h"}, &out, &diagnostics); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if out.Len() != 0 || strings.Contains(diagnostics.String(), "not-to-be-logged") {
		t.Fatalf("unexpected help output: stdout=%q stderr=%q", out.String(), diagnostics.String())
	}
}

func TestConfiguredGeocodeAtHTTPBoundary(t *testing.T) {
	for _, channel := range []int{0, 999} {
		t.Run(fmt.Sprint(channel), func(t *testing.T) {
			o := testOptions()
			o.channel = channel
			type observed struct {
				method string
				path   string
				query  url.Values
				ids    string
			}
			requests := make(chan observed, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- observed{r.Method, r.URL.Path, r.URL.Query(), r.Header.Get("X-GOOG-MAPS-EXPERIENCE-ID")}
				// Untrusted metadata must not make it into the metric summary.
				w.Header().Set("x-goog-maps-metro-area", "fictional-sensitive-api-key")
				fmt.Fprint(w, localResponse)
			}))
			defer server.Close()
			var out, diagnostic bytes.Buffer
			if err := geocode(context.Background(), o, server.URL, &out, &diagnostic); err != nil {
				t.Fatal(err)
			}
			if out.String() != "Fictional fixture address: 1.250000,2.500000\n" {
				t.Fatalf("unexpected derived result: %q", out.String())
			}
			if diagnostic.String() != "metric path=/maps/api/geocode/json status=200 error=false\n" {
				t.Fatalf("unexpected metrics: %q", diagnostic.String())
			}
			got := <-requests
			if got.method != http.MethodGet || got.path != "/maps/api/geocode/json" || got.query.Get("address") != o.address {
				t.Fatalf("incorrect service request: method=%s path=%s address=%q", got.method, got.path, got.query.Get("address"))
			}
			if got.ids != "client-example,request-geocode" {
				t.Fatalf("combined experience IDs = %q", got.ids)
			}
			if got.query.Get("key") != o.key || got.query.Get("channel") != fmt.Sprint(channel) || got.query.Get("client") != "" || got.query.Get("signature") != "" {
				t.Fatal("API-key Geocoding request has incorrect credentials/channel")
			}
		})
	}
}

func TestCLIUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-address", "Fictional fixture address"},
		{"-local", "-address", " \t"},
		{"-local", "-key", "sensitive-key"},
		{"-local", "-channel", "-1"},
		{"-local", "-channel", "1000"},
		{"-local", "-channel", "not-a-number"},
		{"-local", "-request_timeout", "0"},
		{"-local", "-http_timeout", "0"},
		{"-local", "-request_timeout", "-1s"},
		{"-local", "unexpected"},
		{"-request_timeout", "not-a-duration"},
	} {
		var out, diagnostic bytes.Buffer
		if code := run(context.Background(), args, &out, &diagnostic); code != 2 {
			t.Fatalf("args %q: exit = %d, want 2", args, code)
		}
		if out.Len() != 0 || strings.Contains(diagnostic.String(), "sensitive-") || strings.Contains(diagnostic.String(), "metric path=") {
			t.Fatalf("unsafe usage output: stdout=%q stderr=%q", out.String(), diagnostic.String())
		}
	}
}

func TestResponseErrorsAndMetricBoundary(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		metric  string
		wantErr bool
	}{
		{"service-denied", 200, `{"status":"REQUEST_DENIED","error_message":"fictional-sensitive-api-key https://example.invalid/?key=secret"}`, "", true},
		{"malformed-json", 503, `{"fictional-sensitive-api-key":`, "metric path=/maps/api/geocode/json status=503 error=true\n", true},
		{"zero-results", 200, `{"status":"ZERO_RESULTS","results":[]}`, "metric path=/maps/api/geocode/json status=200 error=false\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			var out, diagnostic bytes.Buffer
			err := geocode(context.Background(), testOptions(), server.URL, &out, &diagnostic)
			if tc.wantErr {
				if err == nil || err.Error() != "geocoding failed" {
					t.Fatalf("error = %v, want sanitized service failure", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if out.Len() != 0 || strings.Contains(diagnostic.String(), testOptions().key) || (tc.metric != "" && diagnostic.String() != tc.metric) {
				t.Fatalf("output=%q metrics=%q, want %q", out.String(), diagnostic.String(), tc.metric)
			}
		})
	}
}

func TestRequestCancellationAndIndependentTimeouts(t *testing.T) {
	for _, mode := range []string{"parent-cancel", "request-deadline", "http-timeout"} {
		t.Run(mode, func(t *testing.T) {
			// The harness can always release the handler even if the behavior
			// under test regresses. No sleep or elapsed-time assertion is needed.
			harness, stopHarness := context.WithTimeout(context.Background(), 15*time.Second)
			started := make(chan struct{})
			canceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(canceled)
				case <-harness.Done():
				}
			}))
			defer func() {
				stopHarness()
				server.Close()
			}()
			ctx, cancel := context.WithCancel(harness)
			defer cancel()
			o := testOptions()
			o.httpTimeout, o.requestTimeout = 10*time.Second, 10*time.Second
			want := "geocoding failed: deadline exceeded"
			switch mode {
			case "parent-cancel":
				want = "geocoding failed: request canceled"
			case "request-deadline":
				o.requestTimeout = time.Second
			case "http-timeout":
				o.httpTimeout = time.Second
			}
			var out, diagnostic bytes.Buffer
			done := make(chan error, 1)
			go func() { done <- geocode(ctx, o, server.URL, &out, &diagnostic) }()
			select {
			case <-started:
			case <-harness.Done():
				t.Fatal("HTTP request never reached the fixture")
			}
			if mode == "parent-cancel" {
				cancel() // Synchronize cancellation with an in-flight HTTP request.
			}
			select {
			case err := <-done:
				if err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
			case <-harness.Done():
				t.Fatal("request did not finish before independent harness cancellation")
			}
			select {
			case <-canceled:
			case <-harness.Done():
				t.Fatal("fixture did not observe HTTP request cancellation")
			}
			if out.Len() != 0 || diagnostic.String() != "metric path=/maps/api/geocode/json status=0 error=true\n" {
				t.Fatalf("unexpected canceled request output: stdout=%q metrics=%q", out.String(), diagnostic.String())
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) { return 0, errors.New("private sink failure") }

func TestOutputFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(localHandler))
	defer server.Close()
	for _, metricFailure := range []bool{false, true} {
		var result, diagnostic io.Writer = &bytes.Buffer{}, &bytes.Buffer{}
		want := "writing results failed"
		if metricFailure {
			diagnostic = failingWriter{}
			want = "writing metrics failed"
		} else {
			result = failingWriter{}
		}
		if err := geocode(context.Background(), testOptions(), server.URL, result, diagnostic); err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	var diagnostic bytes.Buffer
	if code := run(context.Background(), []string{"-local"}, failingWriter{}, &diagnostic); code != 1 {
		t.Fatalf("result writer failure exit = %d, want 1", code)
	}
}
