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

// More information about Google Geolocation API is available on
// https://developers.google.com/maps/documentation/geolocation

package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestGeolocation(t *testing.T) {

	// Denver, the mile high city
	response := `{
		"location" : {
			"lat" : 39.73915360,
			"lng" : -104.98470340
		},
		"accuracy" : 4.771975994110107
	}`

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &GeolocationRequest{}

	resp, err := c.Geolocate(context.Background(), r)
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	correctResponse := GeolocationResult{
		Location: LatLng{
			Lat: 39.73915360,
			Lng: -104.98470340,
		},
		Accuracy: 4.771975994110107,
	}

	if !reflect.DeepEqual(*resp, correctResponse) {
		t.Errorf("expected %+v, was %+v", correctResponse, resp)
	}
}

func TestGeolocationError(t *testing.T) {

	// An error response
	response := `{
		"error": {
			"errors": [
				{
					"domain": "global",
					"reason": "invalid",
					"message": "Invalid value for UnsignedInteger: ",
					"locationType": "other",
					"location": "homeMobileCountryCode"
				}
			],
			"code": 400,
			"message": "Invalid value for UnsignedInteger: "
		}
	}`

	server := mockServer(400, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &GeolocationRequest{}

	result, err := c.Geolocate(context.Background(), r)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected HTTPError, got %T: %v", err, err)
	}
	if result != nil || httpErr.StatusCode != 400 || httpErr.Code != 400 ||
		httpErr.Message != "Invalid value for UnsignedInteger: " {
		t.Fatalf("lost Geolocation error metadata: result=%+v, error=%+v", result, httpErr)
	}
	var fieldErrors []struct {
		Domain, Reason, Message, LocationType, Location string
	}
	if err := json.Unmarshal(httpErr.Errors, &fieldErrors); err != nil {
		t.Fatal(err)
	}
	if len(fieldErrors) != 1 || fieldErrors[0].Domain != "global" || fieldErrors[0].Reason != "invalid" ||
		fieldErrors[0].Message != httpErr.Message || fieldErrors[0].LocationType != "other" ||
		fieldErrors[0].Location != "homeMobileCountryCode" {
		t.Fatalf("lost Geolocation field error: %+v", fieldErrors)
	}
	var geoErr *GeolocationError
	if !errors.As(err, &geoErr) || geoErr.Code != httpErr.Code ||
		len(geoErr.Errors) != 1 || geoErr.Errors[0].Reason != fieldErrors[0].Reason ||
		geoErr.Errors[0].Domain != fieldErrors[0].Domain ||
		geoErr.Errors[0].Message != fieldErrors[0].Message {
		t.Fatalf("lost typed Geolocation metadata: %+v", geoErr)
	}
}

func TestGeolocationServiceErrorWithSuccessfulHTTPStatus(t *testing.T) {
	server := mockServer(200, `{"location":{"lat":1,"lng":2},"accuracy":3,"error":{"code":400,"message":"Invalid request"}}`)
	defer server.Close()
	c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Geolocate(context.Background(), &GeolocationRequest{})
	if result != nil || err == nil {
		t.Fatalf("service failure returned result=%+v, error=%v", result, err)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		t.Fatalf("HTTP 200 service failure was classified as an HTTP failure: %+v", httpErr)
	}
	var geoErr *GeolocationError
	if !errors.As(err, &geoErr) || err != geoErr || geoErr.Code != 400 ||
		geoErr.Message != "Invalid request" || err.Error() != geoErr.Message {
		t.Fatalf("expected direct Geolocation service error, got %T: %v", err, err)
	}
}

func TestGeolocationStructuredFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   int
		reason string
	}{
		{"invalid key", 400, 400, "keyInvalid"},
		{"parse error", 400, 400, "parseError"},
		{"daily limit", 403, 403, "dailyLimitExceeded"},
		{"user rate limit", 403, 403, "userRateLimitExceeded"},
		{"not found", 404, 404, "notFound"},
		{"independent service code", 403, 429, "userRateLimitExceeded"},
		{"legacy envelope", 200, 400, "parseError"},
		{"reason without service code", 400, 0, "parseError"},
		{"legacy reason without service code", 200, 0, "parseError"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"code":%d,"message":"Request could not be completed","errors":[{"domain":"geolocation","reason":%q,"message":"Primary failure","locationType":"parameter","location":"key"},{"domain":"usageLimits","reason":"quotaExceeded","message":"Secondary failure"}],"details":[{"extra":"preserved"}]}}`, tt.code, tt.reason)
			server := mockServer(tt.status, body)
			defer server.Close()
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.Geolocate(context.Background(), &GeolocationRequest{})
			var geoErr *GeolocationError
			if result != nil || !errors.As(err, &geoErr) {
				t.Fatalf("expected Geolocation failure, got result=%+v, error=%v", result, err)
			}
			if geoErr.Code != tt.code || geoErr.Message != "Request could not be completed" ||
				geoErr.Error() != geoErr.Message || len(geoErr.Errors) != 2 {
				t.Fatalf("lost service metadata: %+v", geoErr)
			}
			if geoErr.Errors[0].Domain != "geolocation" || geoErr.Errors[0].Reason != tt.reason ||
				geoErr.Errors[0].Message != "Primary failure" || geoErr.Errors[1].Domain != "usageLimits" ||
				geoErr.Errors[1].Reason != "quotaExceeded" || geoErr.Errors[1].Message != "Secondary failure" {
				t.Fatalf("lost reason entries: %+v", geoErr.Errors)
			}
			var httpErr *HTTPError
			if tt.status == 200 {
				if errors.As(err, &httpErr) || err != geoErr {
					t.Fatalf("expected direct service error, got %T: %v", err, err)
				}
			} else {
				if !errors.As(err, &httpErr) || err != httpErr || httpErr.StatusCode != tt.status ||
					httpErr.Code != tt.code || errors.Unwrap(httpErr) != geoErr {
					t.Fatalf("lost HTTP/service distinction: %T: %v", err, err)
				}
				var raw struct {
					Location string
				}
				var entries []json.RawMessage
				if json.Unmarshal(httpErr.Errors, &entries) != nil || len(entries) != 2 ||
					json.Unmarshal(entries[0], &raw) != nil || raw.Location != "key" ||
					string(httpErr.Details) != `[{"extra":"preserved"}]` {
					t.Fatalf("lost extra wire metadata: %+v", httpErr)
				}
			}
		})
	}
}

func TestGeolocationUnstructuredFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"HTML", 403, "<html>Access denied</html>"},
		{"empty", 404, ""},
		{"malformed error", 400, `{"error":{"code":400,`},
		{"invalid reason list", 400, `{"error":{"code":400,"errors":"invalid"}}`},
		{"legacy generic error", 403, `{"status":"REQUEST_DENIED","error_message":"Denied"}`},
		{"malformed success", 200, `{"location":`},
		{"invalid success shape", 200, `{"accuracy":"not a number"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServer(tt.status, tt.body)
			defer server.Close()
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
			if err != nil {
				t.Fatal(err)
			}
			result, err := c.Geolocate(context.Background(), &GeolocationRequest{})
			var geoErr *GeolocationError
			if result != nil || err == nil || errors.As(err, &geoErr) {
				t.Fatalf("fabricated service failure: result=%+v, error=%T: %v", result, err, err)
			}
			var httpErr *HTTPError
			if tt.status != 200 {
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tt.status || errors.Unwrap(httpErr) != nil {
					t.Fatalf("expected generic HTTP error: %T: %v", err, err)
				}
			} else {
				if errors.As(err, &httpErr) {
					t.Fatalf("JSON failure became HTTP failure: %+v", httpErr)
				}
				if tt.name == "invalid success shape" {
					var typeErr *json.UnmarshalTypeError
					if !errors.As(err, &typeErr) {
						t.Fatalf("lost JSON type error: %T: %v", err, err)
					}
				} else if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Fatalf("lost malformed JSON error: %T: %v", err, err)
				}
			}
		})
	}
}

func TestCellTowerAndWiFiRequest(t *testing.T) {
	// Denver, the mile high city
	response := `{
		"location" : {
			"lat" : 39.73915360,
			"lng" : -104.98470340
		},
		"accuracy" : 4.771975994110107
	}`

	server := &countingServer{}

	failResponse := func(reason string, w http.ResponseWriter, r *http.Request) {
		server.failed = append(server.failed, r.URL.RawQuery)
		s := fmt.Sprintf(`{"status":"fail", "message": "%s"}`, reason)
		http.Error(w, s, 999)
	}

	server.s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			failResponse("failed to read body", w, r)
			return
		}
		body := string(b)
		expected := `{"homeMobileCountryCode":310,` +
			`"homeMobileNetworkCode":410,` +
			`"radioType":"gsm",` +
			`"carrier":"Vodafone",` +
			`"considerIp":true,` +
			`"cellTowers":[{"cellId":42,` +
			`"locationAreaCode":415,` +
			`"mobileCountryCode":310,` +
			`"mobileNetworkCode":410,` +
			`"signalStrength":-60,` +
			`"timingAdvance":15}],` +
			`"wifiAccessPoints":[{"macAddress":"00:25:9c:cf:1c:ac",` +
			`"signalStrength":-43,` +
			`"channel":11}]}`
		if body != expected {
			failResponse("failed to parse body", w, r)
			return
		}

		server.successful++

		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		fmt.Fprintln(w, response)
	}))

	defer server.s.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))
	r := &GeolocationRequest{
		HomeMobileCountryCode: 310,
		HomeMobileNetworkCode: 410,
		RadioType:             RadioTypeGSM,
		Carrier:               "Vodafone",
		ConsiderIP:            true,
		CellTowers: []CellTower{{
			CellID:            42,
			LocationAreaCode:  415,
			MobileCountryCode: 310,
			MobileNetworkCode: 410,
			Age:               0,
			SignalStrength:    -60,
			TimingAdvance:     15,
		}},
		WiFiAccessPoints: []WiFiAccessPoint{{
			MACAddress:         "00:25:9c:cf:1c:ac",
			SignalStrength:     -43,
			Age:                0,
			Channel:            11,
			SignalToNoiseRatio: 0,
		}},
	}

	resp, err := c.Geolocate(context.Background(), r)
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	correctResponse := GeolocationResult{
		Location: LatLng{
			Lat: 39.73915360,
			Lng: -104.98470340,
		},
		Accuracy: 4.771975994110107,
	}

	if !reflect.DeepEqual(*resp, correctResponse) {
		t.Errorf("expected %+v, was %+v", correctResponse, resp)
	}
}
