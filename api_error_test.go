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

package maps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

func TestAPIErrorStatuses(t *testing.T) {
	endpoints := []struct {
		name string
		body string
		want interface{}
		call func(*Client) (interface{}, error)
	}{
		{
			name: "Geocode",
			body: `"results":[{"place_id":"place"}]`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.Geocode(context.Background(), &GeocodingRequest{Address: "Sydney"})
				if len(response.Results) == 0 {
					return nil, err
				}
				return response.Results[0].PlaceID, err
			},
		},
		{
			name: "ReverseGeocode",
			body: `"results":[{"place_id":"place"}]`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.ReverseGeocode(context.Background(), &GeocodingRequest{PlaceID: "place"})
				if len(response.Results) == 0 {
					return nil, err
				}
				return response.Results[0].PlaceID, err
			},
		},
		{
			name: "Directions",
			body: `"routes":[{"summary":"route"}]`,
			want: "route",
			call: func(c *Client) (interface{}, error) {
				routes, _, err := c.Directions(context.Background(), &DirectionsRequest{Origin: "Sydney", Destination: "Perth"})
				if len(routes) == 0 {
					return nil, err
				}
				return routes[0].Summary, err
			},
		},
		{
			name: "DistanceMatrix",
			body: `"origin_addresses":["Sydney"],"destination_addresses":["Perth"],"rows":[]`,
			want: "Sydney",
			call: func(c *Client) (interface{}, error) {
				response, err := c.DistanceMatrix(context.Background(), &DistanceMatrixRequest{Origins: []string{"Sydney"}, Destinations: []string{"Perth"}})
				if response == nil || len(response.OriginAddresses) == 0 {
					return nil, err
				}
				return response.OriginAddresses[0], err
			},
		},
		{
			name: "Elevation",
			body: `"results":[{"location":{"lat":1,"lng":2},"elevation":42}]`,
			want: float64(42),
			call: func(c *Client) (interface{}, error) {
				response, err := c.Elevation(context.Background(), &ElevationRequest{Locations: []LatLng{{1, 2}}})
				if len(response) == 0 {
					return nil, err
				}
				return response[0].Elevation, err
			},
		},
		{
			name: "Timezone",
			body: `"dstOffset":0,"rawOffset":0,"timeZoneId":"Etc/UTC","timeZoneName":"UTC"`,
			want: "Etc/UTC",
			call: func(c *Client) (interface{}, error) {
				response, err := c.Timezone(context.Background(), &TimezoneRequest{Location: &LatLng{1, 2}, Timestamp: time.Unix(0, 0)})
				if response == nil || response.TimeZoneID == "" {
					return nil, err
				}
				return response.TimeZoneID, err
			},
		},
		{
			name: "NearbySearch",
			body: `"results":[{"place_id":"place"}]`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.NearbySearch(context.Background(), &NearbySearchRequest{Location: &LatLng{1, 2}, Radius: 10})
				if len(response.Results) == 0 {
					return nil, err
				}
				return response.Results[0].PlaceID, err
			},
		},
		{
			name: "TextSearch",
			body: `"results":[{"place_id":"place"}]`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.TextSearch(context.Background(), &TextSearchRequest{Query: "Sydney"})
				if len(response.Results) == 0 {
					return nil, err
				}
				return response.Results[0].PlaceID, err
			},
		},
		{
			name: "PlaceDetails",
			body: `"result":{"place_id":"place"}`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.PlaceDetails(context.Background(), &PlaceDetailsRequest{PlaceID: "place"})
				if response.PlaceID == "" {
					return nil, err
				}
				return response.PlaceID, err
			},
		},
		{
			name: "QueryAutocomplete",
			body: `"predictions":[{"description":"Sydney"}]`,
			want: "Sydney",
			call: func(c *Client) (interface{}, error) {
				response, err := c.QueryAutocomplete(context.Background(), &QueryAutocompleteRequest{Input: "Sydney"})
				if len(response.Predictions) == 0 {
					return nil, err
				}
				return response.Predictions[0].Description, err
			},
		},
		{
			name: "PlaceAutocomplete",
			body: `"predictions":[{"description":"Sydney"}]`,
			want: "Sydney",
			call: func(c *Client) (interface{}, error) {
				response, err := c.PlaceAutocomplete(context.Background(), &PlaceAutocompleteRequest{Input: "Sydney"})
				if len(response.Predictions) == 0 {
					return nil, err
				}
				return response.Predictions[0].Description, err
			},
		},
		{
			name: "FindPlaceFromText",
			body: `"candidates":[{"place_id":"place"}]`,
			want: "place",
			call: func(c *Client) (interface{}, error) {
				response, err := c.FindPlaceFromText(context.Background(), &FindPlaceFromTextRequest{Input: "Sydney", InputType: FindPlaceFromTextInputTypeTextQuery})
				if len(response.Candidates) == 0 {
					return nil, err
				}
				return response.Candidates[0].PlaceID, err
			},
		},
	}
	statuses := []struct {
		name    string
		status  string
		message string
	}{
		{"quota", "OVER_QUERY_LIMIT", "Quota exceeded"},
		{"daily limit", "OVER_DAILY_LIMIT", "Daily limit exceeded"},
		{"denied", "REQUEST_DENIED", "Access denied"},
		{"server failure", "UNKNOWN_ERROR", "Service failed"},
		{"future status", "FUTURE_STATUS", "A new failure"},
		{"absent message", "OVER_QUERY_LIMIT", ""},
		{"empty status", "", "No status supplied"},
		{"success", "OK", ""},
		{"zero results", "ZERO_RESULTS", ""},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, status := range statuses {
				t.Run(status.name, func(t *testing.T) {
					body := fmt.Sprintf(`{"status":%q`, status.status)
					if status.message != "" {
						body += fmt.Sprintf(`,"error_message":%q`, status.message)
					}
					if status.status != "ZERO_RESULTS" {
						body += "," + endpoint.body
					}
					body += "}"
					server := mockServer(200, body)
					defer server.Close()
					c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
					if err != nil {
						t.Fatal(err)
					}
					result, err := endpoint.call(c)
					if status.status == "OK" || status.status == "ZERO_RESULTS" {
						var want interface{}
						if status.status == "OK" {
							want = endpoint.want
						}
						if err != nil || result != want {
							t.Fatalf("result=%v, error=%v; want result=%v and no error", result, err, want)
						}
						return
					}
					var apiErr *APIError
					if !errors.As(err, &apiErr) {
						t.Fatalf("error=%T: %v, want *APIError", err, err)
					}
					if result != nil || apiErr.Status != status.status || apiErr.Message != status.message {
						t.Fatalf("result=%v, error=%+v; want no result, status=%q and message=%q", result, apiErr, status.status, status.message)
					}
				})
			}
		})
	}
}

func TestAPIErrorDoesNotReplaceRequestFailures(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		cancel bool
		want   error
	}{
		{"malformed JSON", `{`, false, io.ErrUnexpectedEOF},
		{"canceled request", `{"status":"OK","results":[]}`, true, context.Canceled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := mockServer(200, test.body)
			defer server.Close()
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if test.cancel {
				cancel()
			}
			response, err := c.Geocode(ctx, &GeocodingRequest{Address: "Sydney"})
			if !errors.Is(err, test.want) || len(response.Results) != 0 {
				t.Fatalf("result=%+v, error=%v; want no results and %v", response, err, test.want)
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				t.Fatalf("request failure became an API status error: %+v", apiErr)
			}
		})
	}
}
