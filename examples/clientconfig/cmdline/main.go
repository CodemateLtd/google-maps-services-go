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

// Command clientconfig demonstrates transport ownership, bounded requests,
// experience IDs, Geocoding usage channels, and the existing metrics interface.
// Use -local for an explicit offline fixture, never as a production endpoint.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"googlemaps.github.io/maps"
	"googlemaps.github.io/maps/metrics"
)

const (
	localKey      = "fictional-local-api-key"
	localResponse = `{"status":"OK","results":[{"formatted_address":"Fictional fixture address","geometry":{"location":{"lat":1.25,"lng":2.5}}}]}`
)

type options struct {
	local          bool
	key            string
	channel        int
	address        string
	httpTimeout    time.Duration
	requestTimeout time.Duration
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	var o options
	flags := flag.NewFlagSet("clientconfig", flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.BoolVar(&o.local, "local", false, "Use only an in-process HTTP fixture and a fictional API key; no Google request.")
	flags.StringVar(&o.key, "key", "", "API key for a real Google Geocoding request; omit with -local.")
	flags.IntVar(&o.channel, "channel", 0, "Geocoding usage channel, 0 through 999; channels are not supported by every Maps API.")
	flags.StringVar(&o.address, "address", "", "Address to geocode; required for real requests, optional in local mode.")
	flags.DurationVar(&o.httpTimeout, "http_timeout", 5*time.Second, "Positive timeout for the owned HTTP client, including response body reads.")
	flags.DurationVar(&o.requestTimeout, "request_timeout", 3*time.Second, "Positive deadline for this request's context.")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments; use -h for help")
		return 2
	}
	if o.local {
		if o.key != "" {
			fmt.Fprintln(errOut, "-local uses a fictional API key; omit -key")
			return 2
		}
		o.key = localKey
		if o.address == "" {
			o.address = "Fictional fixture address"
		}
	}
	if err := o.validate(); err != nil {
		fmt.Fprintln(errOut, err)
		return 2
	}

	baseURL := ""
	if o.local {
		// WithBaseURL is deliberately limited to our own loopback fixture. There
		// is no arbitrary endpoint flag that could receive real credentials.
		fixture := httptest.NewServer(http.HandlerFunc(localHandler))
		defer fixture.Close()
		baseURL = fixture.URL
	}
	if err := geocode(ctx, o, baseURL, out, errOut); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

func (o options) validate() error {
	if o.httpTimeout <= 0 || o.requestTimeout <= 0 {
		return errors.New("-http_timeout and -request_timeout must be positive")
	}
	if strings.TrimSpace(o.address) == "" {
		return errors.New("specify -address")
	}
	if o.key == "" {
		return errors.New("specify -key or -local")
	}
	// Geocoding supports numeric channels with API keys. Do not generalize
	// that support to every API: see the reporting guide's supported list.
	// https://developers.google.com/maps/reporting-and-monitoring/reporting#usage-tracking-per-channel
	if o.channel < 0 || o.channel > 999 {
		return errors.New("-channel must be between 0 and 999 for Geocoding")
	}
	return nil
}

func geocode(ctx context.Context, o options, baseURL string, out, metricOut io.Writer) error {
	// The SDK wraps the supplied client's Transport. Own both objects rather
	// than mutating http.DefaultClient or the shared http.DefaultTransport.
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: o.httpTimeout}
	reporter := &requestMetrics{}
	clientOptions := []maps.ClientOption{
		maps.WithAPIKey(o.key),
		maps.WithChannel(strconv.Itoa(o.channel)),
		maps.WithHTTPClient(httpClient),
		maps.WithExperienceId("client-example"),
		maps.WithMetricReporter(reporter),
	}
	if baseURL != "" {
		clientOptions = append(clientOptions, maps.WithBaseURL(baseURL))
	}
	client, err := maps.NewClient(clientOptions...)
	if err != nil {
		return errors.New("invalid client configuration")
	}

	requestCtx, cancel := context.WithTimeout(ctx, o.requestTimeout)
	defer cancel()
	// Context IDs APPEND to the client's IDs; they do not replace them. Never
	// change shared client metadata to customize an individual request.
	requestCtx = maps.ExperienceIdContext(requestCtx, "request-geocode")
	response, serviceErr := client.Geocode(requestCtx, &maps.GeocodingRequest{Address: o.address})
	if err := reporter.write(metricOut); err != nil {
		return errors.New("writing metrics failed")
	}
	if serviceErr != nil {
		// HTTP errors may contain the entire signed URL, and API error messages
		// may echo credentials. Neither belongs in this sample's output.
		if errors.Is(serviceErr, context.Canceled) {
			return errors.New("geocoding failed: request canceled")
		}
		if errors.Is(serviceErr, context.DeadlineExceeded) {
			return errors.New("geocoding failed: deadline exceeded")
		}
		return errors.New("geocoding failed")
	}
	for _, result := range response.Results {
		if _, err := fmt.Fprintf(out, "%s: %.6f,%.6f\n", result.FormattedAddress, result.Geometry.Location.Lat, result.Geometry.Location.Lng); err != nil {
			return errors.New("writing results failed")
		}
	}
	return nil
}

// requestMetrics records one sequential SDK call. A reporter used by a shared,
// concurrent client would need synchronized storage or a concurrency-safe sink.
// The SDK reports transport/JSON errors here, not later API-status errors. Only
// the fixed service path, HTTP status, and error presence are retained: never
// the request URL, credentials, raw errors, response body, or metro metadata.
type requestMetrics struct {
	path   string
	status int
	failed bool
}

func (m *requestMetrics) NewRequest(name string) metrics.Request {
	m.path = name
	return m
}

func (m *requestMetrics) EndRequest(_ context.Context, err error, response *http.Response, _ string) {
	m.failed = err != nil
	if response != nil {
		m.status = response.StatusCode
	}
}

func (m *requestMetrics) write(out io.Writer) error {
	_, err := fmt.Fprintf(out, "metric path=%s status=%d error=%t\n", m.path, m.status, m.failed)
	return err
}

func localHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/maps/api/geocode/json" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, localResponse)
}
