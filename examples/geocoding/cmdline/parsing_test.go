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
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestGeocodingInvalidCLI(t *testing.T) {
	server, queries := geocodingServer(t, geocodingFixture)
	for _, tt := range []struct {
		name   string
		inputs []string
	}{
		{"components", []string{"country", "country:", ":US", ":", "country:US:CA", "country:US|", "|country:US", "country:US||route:Main", "unknown:value", "country:US|locality"}},
		{"bounds", []string{"1,2", "1,2|3,4|5,6", "|3,4", "1,2|", "|", "1|3,4", "1,2|3", "1,2,3|4,5", "1,2|3,4,5", ",2|3,4", "1,|3,4", "1,2|,4", "1,2|3,", "bad,2|3,4", "1,bad|3,4", "1,2|bad,4", "1,2|3,bad"}},
		{"latlng", []string{"1", "1,2,3", ",2", "1,", ",", "bad,2", "1,bad", "1|2"}},
	} {
		for _, input := range tt.inputs {
			t.Run(tt.name+"="+input, func(t *testing.T) {
				var stdout bytes.Buffer
				code, err, _ := invokeGeocoding(t, server, []string{"-key=fixture", "-address=Example", "-" + tt.name + "=" + input}, &stdout)
				if code != 2 || err == nil || !strings.Contains(strings.ToLower(err.Error()), tt.name) || stdout.Len() != 0 {
					t.Fatalf("run = (%d, %v), stdout %q", code, err, stdout.String())
				}
			})
		}
	}
	if len(queries) != 0 {
		t.Fatalf("invalid inputs made %d requests", len(queries))
	}
}

func TestGeocodingOptionsAndRunIsolation(t *testing.T) {
	server, queries := geocodingServer(t, geocodingFixture)
	var stdout bytes.Buffer
	code, err, stderr := invokeGeocoding(t, server, []string{
		"-key=fixture", "-components=route:Main Street|locality:Example City|administrative_area:Example State|postal_code:00123|country:US|country:CA",
		"-bounds=-12.5,-45.25|23.75,67.125", "-latlng=-12.5,67.125", "-language=fr", "-region=ca",
		"-result_type=street_address|postal_code", "-location_type=ROOFTOP|RANGE_INTERPOLATED|GEOMETRIC_CENTER|APPROXIMATE|ignored", "-enable_address_descriptor=True",
	}, &stdout)
	if code != 0 || err != nil || stderr != "" {
		t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
	}
	if len(queries) != 1 {
		t.Fatalf("requests = %d, want 1", len(queries))
	}
	q := <-queries
	components := strings.Split(q.Get("components"), "|")
	sort.Strings(components)
	want := []string{"administrative_area:Example State", "country:CA", "locality:Example City", "postal_code:00123", "route:Main Street"}
	if !reflect.DeepEqual(components, want) {
		t.Errorf("components = %v, want %v (last duplicate wins)", components, want)
	}
	for key, want := range map[string]string{
		"bounds": "-12.5,-45.25|23.75,67.125", "latlng": "-12.5,67.125", "language": "fr", "region": "ca",
		"result_type": "street_address|postal_code", "location_type": "ROOFTOP|RANGE_INTERPOLATED|GEOMETRIC_CENTER|APPROXIMATE", "enable_address_descriptor": "true",
	} {
		if q.Get(key) != want {
			t.Errorf("%s = %q, want %q", key, q.Get(key), want)
		}
	}
	stdout.Reset()
	code, err, _ = invokeGeocoding(t, server, []string{"-key=second", "-address=Second address", "-components=", "-bounds=", "-latlng=", "-enable_address_descriptor=False"}, &stdout)
	if code != 0 || err != nil || len(queries) != 1 {
		t.Fatalf("second run = (%d, %v), requests %d", code, err, len(queries))
	}
	q = <-queries
	if q.Get("address") != "Second address" || q.Get("key") != "second" {
		t.Errorf("second request = %v", q)
	}
	for _, key := range []string{"components", "bounds", "latlng", "language", "region", "result_type", "location_type", "enable_address_descriptor"} {
		if _, exists := q[key]; exists {
			t.Errorf("second run retained %s: %v", key, q)
		}
	}
}

func TestGeocodingHelpAndUsage(t *testing.T) {
	server, queries := geocodingServer(t, geocodingFixture)
	for _, tt := range []struct {
		args []string
		code int
	}{
		{[]string{"-help"}, 0},
		{[]string{"-unknown"}, 2},
		{[]string{"-key=fixture", "-address=Example", "extra"}, 2},
		{[]string{"-address=Example"}, 2},
		{[]string{"-client_id=legacy", "-address=Example"}, 2},
		{[]string{"-signature=c2VjcmV0", "-address=Example"}, 2},
		{[]string{"-client_id=legacy", "-signature=!", "-address=Example"}, 2},
		{[]string{"-key=fixture"}, 2},
	} {
		var stdout bytes.Buffer
		code, err, _ := invokeGeocoding(t, server, tt.args, &stdout)
		if code != tt.code || (tt.code == 0 && err != nil) || stdout.Len() != 0 {
			t.Errorf("run(%v) = (%d, %v), stdout %q", tt.args, code, err, stdout.String())
		}
	}
	if len(queries) != 0 {
		t.Errorf("help/usage made %d requests", len(queries))
	}
}

func TestGeocodingCredentialSelection(t *testing.T) {
	server, queries := geocodingServer(t, geocodingFixture)
	for _, args := range [][]string{
		{"-key=preferred", "-client_id=legacy", "-signature=!", "-address=Example"},
		{"-client_id=legacy", "-signature=c2VjcmV0", "-address=Example"},
	} {
		var stdout bytes.Buffer
		code, err, _ := invokeGeocoding(t, server, args, &stdout)
		if code != 0 || err != nil {
			t.Fatalf("credential run = (%d, %v)", code, err)
		}
	}
	if len(queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(queries))
	}
	if q := <-queries; q.Get("key") != "preferred" || q.Get("client") != "" {
		t.Errorf("key precedence lost: %v", q)
	}
	if q := <-queries; q.Get("client") != "legacy" || q.Get("key") != "" {
		t.Errorf("legacy credentials lost: %v", q)
	}
}
