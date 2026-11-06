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
	"net/http"
	"strings"
	"testing"
)

func TestRunFieldsAndIsolation(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK","html_attributions":["Fixture attribution"],"result":{"name":"Sydney Opera House","place_id":"opera-house","formatted_address":"Bennelong Point, Sydney","geometry":{"location":{"lat":-33.856,"lng":151.215}},"rating":4.7,"user_ratings_total":321}}`))
	for _, fields := range []string{"NAME,geometry/location,Formatted_Address,rating", ""} {
		args := []string{"-key=fixture", "-place_id=opera-house"}
		if fields != "" {
			args = append(args, "-fields="+fields)
		}
		var stdout, stderr bytes.Buffer
		code, err := invoke(t, args, &stdout, &stderr, options)
		if code != 0 || err != nil {
			t.Fatalf("run = (%d, %v)", code, err)
		}
		if len(requests) != 1 {
			t.Fatalf("requests = %d, want 1", len(requests))
		}
		query := <-requests
		if query.Get("placeid") != "opera-house" {
			t.Errorf("placeid = %q", query.Get("placeid"))
		}
		if fields == "" {
			if _, present := query["fields"]; present {
				t.Errorf("second run retained fields: %v", query)
			}
		} else if values := query["fields"]; len(values) != 1 || values[0] != "name,geometry/location,formatted_address,rating" {
			t.Errorf("fields = %v, want parsed canonical masks", values)
		}
		if _, present := query["sessiontoken"]; present {
			t.Errorf("details invented session token: %v", query)
		}
		for _, value := range []string{"Sydney Opera House", "opera-house", "Bennelong Point, Sydney", "-33.856", "151.215", "321", "Fixture attribution"} {
			if !strings.Contains(stdout.String(), value) {
				t.Errorf("output lacks decoded %q: %s", value, &stdout)
			}
		}
	}
}
