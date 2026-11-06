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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"googlemaps.github.io/maps"
)

func TestGeolocationCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		request  maps.GeolocationRequest
		response string
		lat      float64
		lng      float64
		accuracy float64
	}{
		{
			name:     "explicit IP fallback",
			input:    `{"considerIp":true}`,
			request:  maps.GeolocationRequest{ConsiderIP: true},
			response: `{"location":{"lat":39.7391536,"lng":-104.9847034},"accuracy":1250.75}`,
			lat:      39.7391536,
			lng:      -104.9847034,
			accuracy: 1250.75,
		},
		{
			name:  "structured measurements without IP fallback",
			input: `{"considerIp":false,"homeMobileCountryCode":999,"homeMobileNetworkCode":99,"radioType":"gsm","carrier":"Fictional network","cellTowers":[{"cellId":42,"locationAreaCode":7,"mobileCountryCode":999,"mobileNetworkCode":99,"age":125,"signalStrength":-65,"timingAdvance":3}],"wifiAccessPoints":[{"macAddress":"02:00:00:00:00:01","signalStrength":-45.5,"age":20,"channel":6,"signalToNoiseRatio":25.25},{"macAddress":"02:00:00:00:00:02","signalStrength":-60,"channel":11}]}`,
			request: maps.GeolocationRequest{
				HomeMobileCountryCode: 999,
				HomeMobileNetworkCode: 99,
				RadioType:             maps.RadioTypeGSM,
				Carrier:               "Fictional network",
				ConsiderIP:            false,
				CellTowers: []maps.CellTower{{
					CellID:            42,
					LocationAreaCode:  7,
					MobileCountryCode: 999,
					MobileNetworkCode: 99,
					Age:               125,
					SignalStrength:    -65,
					TimingAdvance:     3,
				}},
				WiFiAccessPoints: []maps.WiFiAccessPoint{
					{MACAddress: "02:00:00:00:00:01", SignalStrength: -45.5, Age: 20, Channel: 6, SignalToNoiseRatio: 25.25},
					{MACAddress: "02:00:00:00:00:02", SignalStrength: -60, Channel: 11},
				},
			},
			response: `{"location":{"lat":-33.8688,"lng":151.2093},"accuracy":4.771975994110107}`,
			lat:      -33.8688,
			lng:      151.2093,
			accuracy: 4.771975994110107,
		},
		{
			name:  "false and zero values are not replaced",
			input: `{"considerIp":false,"homeMobileCountryCode":0,"homeMobileNetworkCode":0,"cellTowers":[{"cellId":0,"locationAreaCode":0,"mobileCountryCode":0,"mobileNetworkCode":0,"age":0,"signalStrength":0,"timingAdvance":0}],"wifiAccessPoints":[{"macAddress":"02:00:00:00:00:01","signalStrength":0,"age":0,"channel":0,"signalToNoiseRatio":0}]} `,
			request: maps.GeolocationRequest{
				ConsiderIP:       false,
				CellTowers:       []maps.CellTower{{}},
				WiFiAccessPoints: []maps.WiFiAccessPoint{{MACAddress: "02:00:00:00:00:01"}},
			},
			response: `{"location":{"lat":0,"lng":0},"accuracy":0}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				if r.Method != http.MethodPost || r.URL.Path != "/geolocation/v1/geolocate" {
					t.Errorf("request = %s %s, want Geolocation POST", r.Method, r.URL.Path)
				}
				if keys := r.URL.Query()["key"]; len(keys) != 1 || keys[0] != "offline-fixture-key" {
					t.Errorf("API key query = %v, want fixture key", keys)
				}
				var body bytes.Buffer
				if _, err := body.ReadFrom(r.Body); err != nil {
					t.Errorf("reading request: %v", err)
				}
				var got maps.GeolocationRequest
				if err := json.Unmarshal(body.Bytes(), &got); err != nil {
					t.Errorf("decoding request: %v", err)
				}
				if !reflect.DeepEqual(got, tt.request) {
					t.Errorf("SDK request = %#v, want %#v", got, tt.request)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(body.Bytes(), &fields); err != nil {
					t.Errorf("decoding request fields: %v", err)
				}
				if string(fields["considerIp"]) != fmt.Sprint(tt.request.ConsiderIP) {
					t.Errorf("wire considerIp = %s, want explicit %t", fields["considerIp"], tt.request.ConsiderIP)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tt.response)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := run([]string{"-key", "offline-fixture-key", "-request", tt.input}, &stdout, &stderr, maps.WithBaseURL(server.URL))
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("command exit = %d, stderr = %s", code, &stderr)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("HTTP calls = %d, want 1", got)
			}
			var lat, lng, accuracy float64
			n, err := fmt.Fscanf(&stdout, "latitude: %g\nlongitude: %g\naccuracy_m: %g\n", &lat, &lng, &accuracy)
			if err != nil || n != 3 {
				t.Fatalf("parsing numeric output: fields = %d, error = %v", n, err)
			}
			if lat != tt.lat || lng != tt.lng || accuracy != tt.accuracy {
				t.Errorf("location/accuracy = %g, %g, %g; want %g, %g, %g", lat, lng, accuracy, tt.lat, tt.lng, tt.accuracy)
			}
		})
	}
}

func TestGeolocationInvalidInputBeforeDispatch(t *testing.T) {
	valid := `{"considerIp":false}`
	tests := []struct {
		name  string
		args  []string
		noKey bool
	}{
		{name: "missing key", args: []string{"-request", valid}, noKey: true},
		{name: "blank key", args: []string{"-key", "  ", "-request", valid}, noKey: true},
		{name: "missing request"},
		{name: "missing IP choice", args: []string{"-request", `{}`}},
		{name: "measurements do not imply consent", args: []string{"-request", `{"cellTowers":[{"cellId":42}]}`}},
		{name: "null IP choice", args: []string{"-request", `{"considerIp":null}`}},
		{name: "string IP choice", args: []string{"-request", `{"considerIp":"false"}`}},
		{name: "null request", args: []string{"-request", `null`}},
		{name: "array request", args: []string{"-request", `[]`}},
		{name: "malformed JSON", args: []string{"-request", `{"considerIp":false`}},
		{name: "trailing object", args: []string{"-request", valid + `{}`}},
		{name: "trailing garbage", args: []string{"-request", valid + ` garbage`}},
		{name: "unknown field", args: []string{"-request", `{"considerIp":false,"wifiAccessPoint":[]}`}},
		{name: "wrong cell field type", args: []string{"-request", `{"considerIp":false,"cellTowers":[{"cellId":"42"}]}`}},
		{name: "wrong cell shape", args: []string{"-request", `{"considerIp":false,"cellTowers":{}}`}},
		{name: "wrong WiFi field type", args: []string{"-request", `{"considerIp":false,"wifiAccessPoints":[{"channel":"six"}]}`}},
		{name: "negative unsigned WiFi age", args: []string{"-request", `{"considerIp":false,"wifiAccessPoints":[{"age":-1}]}`}},
		{name: "unexpected positional argument", args: []string{"-request", valid, "extra"}},
		{name: "unknown flag", args: []string{"-unknown"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				fmt.Fprint(w, `{"location":{"lat":0,"lng":0},"accuracy":1}`)
			}))
			defer server.Close()
			args := tt.args
			if !tt.noKey {
				args = append([]string{"-key", "offline-fixture-key"}, args...)
			}
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr, maps.WithBaseURL(server.URL))
			if code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Errorf("invalid input exit = %d, stdout = %s, stderr = %s; want usage error only", code, &stdout, &stderr)
			}
			if got := atomic.LoadInt32(&calls); got != 0 {
				t.Errorf("invalid input reached HTTP %d times", got)
			}
			if strings.Contains(stderr.String(), "offline-fixture-key") {
				t.Error("diagnostic disclosed API key")
			}
		})
	}
}

func TestGeolocationHelp(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-h"}, &stdout, &stderr, maps.WithBaseURL(server.URL))
	if code != 0 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Errorf("help exit = %d, stdout = %s, stderr = %s", code, &stdout, &stderr)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("help reached HTTP %d times", got)
	}
}

func TestGeolocationServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		response string
		want     string
	}{
		{
			name:     "API error envelope",
			status:   http.StatusOK,
			response: `{"error":{"code":404,"message":"No matching measurements","errors":[{"reason":"notFound"}]}}`,
			want:     "No matching measurements",
		},
		{
			name:     "HTTP denial",
			status:   http.StatusForbidden,
			response: `{"error":{"code":403,"message":"Fixture access denied"}}`,
			want:     "Fixture access denied",
		},
		{
			name:     "malformed service response",
			status:   http.StatusOK,
			response: `{"location":`,
			want:     "EOF",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.response)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			code := run([]string{"-key", "offline-fixture-key", "-request", `{"considerIp":true}`}, &stdout, &stderr, maps.WithBaseURL(server.URL))
			if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("service failure exit = %d, stdout = %s, stderr = %s; want %q", code, &stdout, &stderr, tt.want)
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("HTTP calls = %d, want 1", got)
			}
		})
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("offline transport failure")
}

func TestGeolocationTransportErrorDoesNotDiscloseKey(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// This key also tests redaction of the URL-escaped form in transport errors.
	key := "offline fixture+key"
	code := run([]string{"-key", key, "-request", `{"considerIp":true}`}, &stdout, &stderr,
		maps.WithHTTPClient(&http.Client{Transport: failingTransport{}}))
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "offline transport failure") {
		t.Errorf("transport failure exit = %d, stdout = %s, stderr = %s", code, &stdout, &stderr)
	}
	if strings.Contains(stderr.String(), key) || strings.Contains(stderr.String(), "offline+fixture%2Bkey") {
		t.Error("transport error disclosed API key")
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) {
	return 0, fmt.Errorf("private output failure")
}

func TestGeolocationOutputFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"location":{"lat":1,"lng":2},"accuracy":3}`)
	}))
	defer server.Close()
	var stderr bytes.Buffer
	code := run([]string{"-key", "offline-fixture-key", "-request", `{"considerIp":true}`},
		failingOutput{}, &stderr, maps.WithBaseURL(server.URL))
	if code != 1 || strings.Contains(stderr.String(), "offline-fixture-key") {
		t.Errorf("output failure exit = %d, stderr = %s", code, &stderr)
	}
}
