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
	"os"
	"os/exec"
	"testing"

	"googlemaps.github.io/maps"
)

func TestParsePriceLevels(t *testing.T) {
	tests := []struct {
		name    string
		minimum string
		maximum string
		wantMin maps.PriceLevel
		wantMax maps.PriceLevel
	}{
		{"minimum only", "2", "", maps.PriceLevelModerate, ""},
		{"free minimum", "0", "", maps.PriceLevelFree, ""},
		{"free maximum", "", "0", "", maps.PriceLevelFree},
		{"inexpensive maximum", "", "1", "", maps.PriceLevelInexpensive},
		{"moderate maximum", "", "2", "", maps.PriceLevelModerate},
		{"expensive maximum", "", "3", "", maps.PriceLevelExpensive},
		{"very expensive maximum", "", "4", "", maps.PriceLevelVeryExpensive},
		{"distinct bounds", "1", "4", maps.PriceLevelInexpensive, maps.PriceLevelVeryExpensive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request maps.NearbySearchRequest
			parsePriceLevels(tt.minimum, tt.maximum, &request)
			if request.MinPrice != tt.wantMin || request.MaxPrice != tt.wantMax {
				t.Errorf("price bounds = (%q, %q), want (%q, %q)", request.MinPrice, request.MaxPrice, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestInvalidMaximumPrice(t *testing.T) {
	if os.Getenv("GO_NEARBY_PRICE_HELPER") == "1" {
		var request maps.NearbySearchRequest
		parsePriceLevels("1", "not-a-price", &request)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInvalidMaximumPrice$")
	cmd.Env = append(os.Environ(), "GO_NEARBY_PRICE_HELPER=1")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("invalid maximum with a valid minimum: error = %v, want exit status 2; output: %s", err, output)
	}
}
