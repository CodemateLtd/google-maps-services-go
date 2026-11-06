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
	"reflect"
	"testing"

	"googlemaps.github.io/maps"
)

func TestParseComponentsCountries(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		existing map[maps.Component][]string
		want     map[maps.Component][]string
	}{
		{
			name:  "fresh request",
			input: "country:us",
			want:  map[maps.Component][]string{maps.ComponentCountry: {"us"}},
		},
		{
			name:     "existing empty map",
			input:    "country:ca",
			existing: map[maps.Component][]string{},
			want:     map[maps.Component][]string{maps.ComponentCountry: {"ca"}},
		},
		{
			name:  "repeated countries preserve spelling and duplicates",
			input: "country:us|country:CA|country:us",
			want:  map[maps.Component][]string{maps.ComponentCountry: {"us", "CA", "us"}},
		},
		{
			name:  "append without replacing existing entries",
			input: "country:us|country:ca",
			existing: map[maps.Component][]string{
				maps.ComponentCountry:  {"gb"},
				maps.ComponentLocality: {"London"},
			},
			want: map[maps.Component][]string{
				maps.ComponentCountry:  {"gb", "us", "ca"},
				maps.ComponentLocality: {"London"},
			},
		},
		{
			name: "omitted on fresh request",
		},
		{
			name:     "omitted on existing empty map",
			existing: map[maps.Component][]string{},
			want:     map[maps.Component][]string{},
		},
		{
			name:     "omitted preserves existing entries",
			existing: map[maps.Component][]string{maps.ComponentCountry: {"us"}},
			want:     map[maps.Component][]string{maps.ComponentCountry: {"us"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &maps.PlaceAutocompleteRequest{Components: tt.existing}
			if err := parseComponents(tt.input, r); err != nil {
				t.Fatalf("parseComponents(%q): %v", tt.input, err)
			}
			if !reflect.DeepEqual(r.Components, tt.want) {
				t.Errorf("components = %#v, want %#v", r.Components, tt.want)
			}
		})
	}
}

func TestParseComponentsRejectsInvalidTokens(t *testing.T) {
	for _, input := range []string{
		"country",
		"us",
		":us",
		"country:",
		":",
		"country:us:ca",
		"country:us:",
		"locality:Paris",
		"unknown:value",
		"|",
		"|country:us",
	} {
		t.Run(input, func(t *testing.T) {
			r := &maps.PlaceAutocompleteRequest{}
			if err := parseComponents(input, r); err == nil {
				t.Fatalf("parseComponents(%q) accepted invalid input", input)
			}
			if r.Components != nil {
				t.Errorf("invalid first token initialized components: %#v", r.Components)
			}
		})
	}
	for _, input := range []string{"country:us|", "country:us||country:ca", "country:us|country", "country:us|country:", "country:us|unknown:value"} {
		t.Run(input, func(t *testing.T) {
			r := &maps.PlaceAutocompleteRequest{}
			if err := parseComponents(input, r); err == nil {
				t.Fatalf("parseComponents(%q) accepted invalid later token", input)
			}
			if got := r.Components[maps.ComponentCountry]; !reflect.DeepEqual(got, []string{"us"}) {
				t.Errorf("countries before invalid token = %#v, want [us]", got)
			}
		})
	}
}
