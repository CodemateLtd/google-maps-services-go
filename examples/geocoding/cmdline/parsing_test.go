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
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"googlemaps.github.io/maps"
)

func TestGeocodingParsersRejectMalformedInput(t *testing.T) {
	tests := []struct {
		name   string
		parse  func(string, *maps.GeocodingRequest) error
		inputs []string
	}{
		{
			name:  "components",
			parse: parseComponents,
			inputs: []string{
				"country", "country:", ":US", ":", "country:US:CA",
				"country:US|", "|country:US", "country:US||route:Main",
				"unknown:value", "country:US|locality",
			},
		},
		{
			name:  "bounds",
			parse: parseBounds,
			inputs: []string{
				"1,2", "1,2|3,4|5,6", "|3,4", "1,2|", "|",
				"1|3,4", "1,2|3", "1,2,3|4,5", "1,2|3,4,5",
				",2|3,4", "1,|3,4", "1,2|,4", "1,2|3,",
				"bad,2|3,4", "1,bad|3,4", "1,2|bad,4", "1,2|3,bad",
			},
		},
		{
			name:  "latlng",
			parse: parseLatLng,
			inputs: []string{
				"1", "1,2,3", ",2", "1,", ",", "bad,2", "1,bad", "1|2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, input := range tt.inputs {
				t.Run(input, func(t *testing.T) {
					err := tt.parse(input, &maps.GeocodingRequest{})
					if err == nil || !strings.Contains(err.Error(), "parse") {
						t.Fatalf("parse(%q) error = %v, want a contextual parsing error", input, err)
					}
				})
			}
		})
	}
}

func TestGeocodingParsersPreserveValidValues(t *testing.T) {
	r := &maps.GeocodingRequest{}
	if err := parseComponents("route:Main Street|locality:Example City|administrative_area:Example State|postal_code:00123|country:US|country:CA", r); err != nil {
		t.Fatal(err)
	}
	wantComponents := map[maps.Component]string{
		maps.ComponentRoute:              "Main Street",
		maps.ComponentLocality:           "Example City",
		maps.ComponentAdministrativeArea: "Example State",
		maps.ComponentPostalCode:         "00123",
		maps.ComponentCountry:            "CA",
	}
	if !reflect.DeepEqual(r.Components, wantComponents) {
		t.Errorf("components = %#v, want %#v (last duplicate wins)", r.Components, wantComponents)
	}
	if err := parseBounds("-12.5,-45.25|23.75,67.125", r); err != nil {
		t.Fatal(err)
	}
	wantBounds := &maps.LatLngBounds{
		SouthWest: maps.LatLng{Lat: -12.5, Lng: -45.25},
		NorthEast: maps.LatLng{Lat: 23.75, Lng: 67.125},
	}
	if !reflect.DeepEqual(r.Bounds, wantBounds) {
		t.Errorf("bounds = %#v, want southwest/northeast %#v", r.Bounds, wantBounds)
	}
	if err := parseLatLng("-12.5,67.125", r); err != nil {
		t.Fatal(err)
	}
	if want := (maps.LatLng{Lat: -12.5, Lng: 67.125}); r.LatLng == nil || *r.LatLng != want {
		t.Errorf("latlng = %#v, want %#v", r.LatLng, want)
	}
	// These parsers validate syntax, not geographic ranges.
	if err := parseLatLng("91,181", r); err != nil {
		t.Fatalf("unexpected geographic policy: %v", err)
	}
	if r.LatLng.Lat != 91 || r.LatLng.Lng != 181 {
		t.Errorf("out-of-range numeric values changed: %#v", r.LatLng)
	}
}

func TestGeocodingEmptyOptionalFlagsAreOmitted(t *testing.T) {
	for _, populated := range []bool{false, true} {
		r := &maps.GeocodingRequest{}
		if populated {
			r.Components = map[maps.Component]string{maps.ComponentCountry: "US"}
			r.Bounds = &maps.LatLngBounds{SouthWest: maps.LatLng{Lat: 1, Lng: 2}, NorthEast: maps.LatLng{Lat: 3, Lng: 4}}
			r.LatLng = &maps.LatLng{Lat: 5, Lng: 6}
		}
		want := *r
		for _, parse := range []func(string, *maps.GeocodingRequest) error{parseComponents, parseBounds, parseLatLng} {
			if err := parse("", r); err != nil {
				t.Fatalf("empty optional flag: %v", err)
			}
		}
		if !reflect.DeepEqual(*r, want) {
			t.Errorf("empty flags changed request: got %#v, want %#v", *r, want)
		}
	}
}

type parsingTransport func(*http.Request) (*http.Response, error)

func (f parsingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestGeocodingInvalidCLI(t *testing.T) {
	if os.Getenv("GO_GEOCODING_PARSING_HELPER") == "1" {
		http.DefaultTransport = parsingTransport(func(*http.Request) (*http.Response, error) {
			fmt.Fprintln(os.Stderr, "unexpected-geocoding-dispatch")
			return nil, fmt.Errorf("network disabled for invalid-input fixture")
		})
		os.Args = []string{os.Args[0], "-key=offline-fixture-key", "-address=fixture", os.Getenv("GO_GEOCODING_INVALID_FLAG")}
		main()
		return
	}
	for _, arg := range []string{
		"-components=country", "-components=country:", "-components=country:US:CA", "-components=unknown:value",
		"-bounds=1,2", "-bounds=1,2|3,4|5,6", "-bounds=1|3,4", "-bounds=1,2|3,", "-bounds=1,2|bad,4",
		"-latlng=1", "-latlng=1,2,3", "-latlng=,2", "-latlng=1,bad",
	} {
		t.Run(arg, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestGeocodingInvalidCLI$")
			cmd.Env = append(os.Environ(), "GO_GEOCODING_PARSING_HELPER=1", "GO_GEOCODING_INVALID_FLAG="+arg)
			output, err := cmd.CombinedOutput()
			exit, ok := err.(*exec.ExitError)
			if !ok || exit.ExitCode() != 1 {
				t.Fatalf("invalid CLI exit = %v, want status 1; output: %s", err, output)
			}
			text := string(output)
			if !strings.Contains(text, "fatal error:") || !strings.Contains(strings.ToLower(text), strings.SplitN(arg[1:], "=", 2)[0]) {
				t.Errorf("missing useful flag error: %s", text)
			}
			if strings.Contains(text, "panic:") || strings.Contains(text, "unexpected-geocoding-dispatch") {
				t.Errorf("malformed input panicked or reached the service: %s", text)
			}
		})
	}
}
