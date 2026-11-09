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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
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

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &GeolocationRequest{}

	_, err := c.Geolocate(context.Background(), r)
	if err == nil {
		t.Errorf("r.Get returned nil error")
	}

	correctResponse := "Invalid value for UnsignedInteger: "

	if !reflect.DeepEqual(err.Error(), correctResponse) {
		t.Errorf("expected %+v, was %+v", correctResponse, err)
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
		var body, want map[string]interface{}
		if err := json.Unmarshal(b, &body); err != nil {
			failResponse("failed to parse body", w, r)
			return
		}
		if err := json.Unmarshal([]byte(expected), &want); err != nil {
			failResponse("invalid expected request", w, r)
			return
		}
		if !reflect.DeepEqual(body, want) {
			failResponse("unexpected request fields", w, r)
			return
		}

		server.successful++

		w.WriteHeader(200)
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		fmt.Fprintln(w, response)
	}))

	defer server.s.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))
	considerIP := true
	r := &GeolocationRequest{
		HomeMobileCountryCode: 310,
		HomeMobileNetworkCode: 410,
		RadioType:             RadioTypeGSM,
		Carrier:               "Vodafone",
		ConsiderIP:            &considerIP,
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

type geolocationRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f geolocationRoundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGeolocationConsiderIP(t *testing.T) {
	considerIPFalse, considerIPTrue := false, true
	tests := []struct {
		name    string
		request GeolocationRequest
		body    string
	}{
		{"omitted without signals", GeolocationRequest{}, `{}`},
		{"explicit false without signals", GeolocationRequest{ConsiderIP: &considerIPFalse}, `{"considerIp":false}`},
		{"explicit true without signals", GeolocationRequest{ConsiderIP: &considerIPTrue}, `{"considerIp":true}`},
		{"omitted with one cell", GeolocationRequest{CellTowers: []CellTower{{CellID: 42}}}, `{"cellTowers":[{"cellId":42}]}`},
		{"omitted with one access point", GeolocationRequest{WiFiAccessPoints: []WiFiAccessPoint{{MACAddress: "00:25:9c:cf:1c:ac"}}}, `{"wifiAccessPoints":[{"macAddress":"00:25:9c:cf:1c:ac"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			calls := 0
			client, err := NewClient(WithAPIKey(apiKey), WithHTTPClient(&http.Client{
				Transport: geolocationRoundTripperFunc(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != "POST" || r.URL.Path != "/geolocation/v1/geolocate" {
						return nil, fmt.Errorf("unexpected Geolocation request: %s %s", r.Method, r.URL.Path)
					}
					var err error
					body, err = ioutil.ReadAll(r.Body)
					if err != nil {
						return nil, err
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": {"application/json"}},
						Body:       ioutil.NopCloser(strings.NewReader(`{"location":{"lat":1.25,"lng":2.5},"accuracy":12}`)),
						Request:    r,
					}, nil
				}),
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := client.Geolocate(ctx, &tt.request)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("Geolocation requests = %d, want 1", calls)
			}
			var got, want map[string]interface{}
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("invalid request JSON %q: %v", body, err)
			}
			if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Geolocation JSON = %s, want %s", body, tt.body)
			}
			if result.Location != (LatLng{Lat: 1.25, Lng: 2.5}) || result.Accuracy != 12 {
				t.Errorf("decoded Geolocation result = %#v", result)
			}
		})
	}
}
