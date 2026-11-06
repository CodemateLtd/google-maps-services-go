// Copyright 2015 Google Inc. All Rights Reserved.
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

package maps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"go.opencensus.io/stats/view"
	"googlemaps.github.io/maps/metrics"
)

func TestClientChannelIsConfigured(t *testing.T) {
	_, err := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithChannel("Test-Channel"))
	if err != nil {
		t.Errorf("Unable to create client with channel")
	}
}

func TestClientWithExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, err := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithExperienceId(ids...))
	assert.Nil(t, err)
	assert.Equal(t, c.experienceId, ids)
}

func TestClientSetExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.setExperienceId(ids...)
	assert.Equal(t, c.experienceId, ids)
}

func TestClientGetExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.experienceId = ids
	assert.Equal(t, c.getExperienceId(), ids)
}

func TestClientClearExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.experienceId = ids
	c.clearExperienceId()
	assert.Nil(t, c.experienceId)
}

func TestClientSetExperienceIdHeader(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	// slice has two elements
	c.experienceId = ids
	req, _ := http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))

	// slice is nil
	c.experienceId = nil
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, "", req.Header.Get(ExperienceIdHeaderName))

	// slice is empty
	c.experienceId = []string{}
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, "", req.Header.Get(ExperienceIdHeaderName))

	var ctx context.Context
	// context has one element
	c.experienceId = []string{}
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, "foo")
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, "foo", req.Header.Get(ExperienceIdHeaderName))

	// context has two elements
	c.experienceId = []string{}
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, ids...)
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))

	// context has two elements and client has two elements
	c.experienceId = ids
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, ids...)
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, strings.Join(ids, ",") + "," + strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))
}

func TestClientExperienceIdSample(t *testing.T) {
	// [START maps_experience_id]
	experienceId := uuid.New().String()

	// instantiate client with experience id
	client, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithExperienceId("foo"))

	// clear the current experience id
	client.clearExperienceId()

	// set a new experience id
	otherExperienceId := uuid.New().String()
	client.setExperienceId(experienceId, otherExperienceId)

	// make API request, the client will set the header
	// X-GOOG-MAPS-EXPERIENCE-ID: experienceId,otherExperienceId

	// get current experience id
	var ids []string
	ids = client.getExperienceId()
	// [END maps_experience_id]

	assert.Equal(t, ids, []string{experienceId, otherExperienceId})
}

type responseBody struct {
	io.Reader
	closed bool
}

func (b *responseBody) Close() error {
	b.closed = true
	return nil
}

type responseTransport struct {
	statusCode int
	body       *responseBody
}

func (t *responseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: t.statusCode, Body: t.body, Header: make(http.Header)}, nil
}

func TestJSONHTTPResponseHandling(t *testing.T) {
	methods := []struct {
		name string
		call func(*Client) error
	}{
		{"GET", func(c *Client) error {
			_, err := c.Geocode(context.Background(), &GeocodingRequest{Address: "Sydney"})
			return err
		}},
		{"POST", func(c *Client) error {
			_, err := c.Geolocate(context.Background(), &GeolocationRequest{})
			return err
		}},
	}
	responses := []struct {
		name        string
		statusCode  int
		body        string
		code        int
		status      string
		message     string
		decodeError bool
	}{
		{
			name:       "success",
			statusCode: 200,
			body:       `{"status":"OK","results":[],"location":{"lat":1,"lng":2},"accuracy":3}`,
		},
		{
			name:        "invalid success JSON",
			statusCode:  200,
			body:        `{`,
			decodeError: true,
		},
		{
			name:       "below success boundary",
			statusCode: 199,
			body:       `{}`,
		},
		{
			name:       "legacy service error",
			statusCode: 500,
			body:       `{"status":"UNKNOWN_ERROR","error_message":"Service failed"}`,
			status:     "UNKNOWN_ERROR",
			message:    "Service failed",
		},
		{
			name:       "independent HTTP and service codes",
			statusCode: 403,
			body:       `{"error":{"statusCode":200,"code":7,"status":"PERMISSION_DENIED","message":"Billing disabled"}}`,
			code:       7,
			status:     "PERMISSION_DENIED",
			message:    "Billing disabled",
		},
		{
			name:       "unknown HTTP status",
			statusCode: 599,
			body:       "upstream failure",
		},
	}
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			for _, response := range responses {
				t.Run(response.name, func(t *testing.T) {
					body := &responseBody{Reader: strings.NewReader(response.body)}
					c, err := NewClient(WithAPIKey(apiKey), WithHTTPClient(&http.Client{
						Transport: &responseTransport{statusCode: response.statusCode, body: body},
					}))
					if err != nil {
						t.Fatal(err)
					}
					err = method.call(c)
					if !body.closed {
						t.Error("response body was not closed")
					}
					if response.decodeError {
						if !errors.Is(err, io.ErrUnexpectedEOF) {
							t.Fatalf("expected JSON decode error, got %v", err)
						}
						return
					}
					if response.statusCode == 200 {
						if err != nil {
							t.Fatal(err)
						}
						return
					}
					var httpErr *HTTPError
					if !errors.As(err, &httpErr) {
						t.Fatalf("expected HTTPError, got %T: %v", err, err)
					}
					if httpErr.StatusCode != response.statusCode || httpErr.Code != response.code ||
						httpErr.Status != response.status || httpErr.Message != response.message {
						t.Fatalf("lost HTTP/service metadata: %+v", httpErr)
					}
				})
			}
		})
	}
}

func TestRoadsHTTPFailureMetrics(t *testing.T) {
	if err := metrics.RegisterViews(); err != nil {
		t.Fatal(err)
	}
	defer view.Unregister(metrics.Count, metrics.Latency)
	responses := []struct {
		code int
		body string
	}{
		{200, `{}`},
		{403, `{"error":{"code":403,"message":"Billing disabled","status":"PERMISSION_DENIED"}}`},
		{503, ""},
		{599, "upstream failure"},
	}
	for _, response := range responses {
		server := mockServer(response.code, response.body)
		defer server.Close()
		c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL), WithMetricReporter(metrics.OpenCensusReporter{}))
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.NearestRoads(context.Background(), &NearestRoadsRequest{Points: []LatLng{{1, 2}}})
		if (err != nil) != (response.code != 200) {
			t.Fatalf("HTTP %d returned error=%v", response.code, err)
		}
	}
	rows, err := view.RetrieveData(metrics.Count.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int64{"200": 1, "403": 1, "503": 1, "599": 1}
	for _, row := range rows {
		var httpCode, apiStatus string
		for _, tag := range row.Tags {
			switch tag.Key.Name() {
			case "http_code":
				httpCode = tag.Value
			case "api_status":
				apiStatus = tag.Value
			}
		}
		count, ok := row.Data.(*view.CountData)
		if !ok || count.Value != wantCounts[httpCode] {
			t.Fatalf("unexpected count for HTTP %s: %+v", httpCode, row.Data)
		}
		if (apiStatus != "") != (httpCode != "200") {
			t.Errorf("HTTP %s recorded the wrong success/failure classification: %q", httpCode, apiStatus)
		}
		delete(wantCounts, httpCode)
	}
	if len(wantCounts) != 0 {
		t.Errorf("missing HTTP request counts: %v", wantCounts)
	}
}
