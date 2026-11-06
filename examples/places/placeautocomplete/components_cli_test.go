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

	"github.com/google/uuid"
)

func TestComponentsCLI(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		include     bool
	}{
		{"single country", "country:us", true},
		{"repeated countries", "country:us|country:ca", true},
		{"spelling and duplicates", "country:us|country:CA|country:us", true},
		{"omitted", "", false},
		{"explicit empty", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK","predictions":[{"description":"Fixture Springfield","place_id":"springfield-id","terms":[{"offset":0,"value":"Springfield"}]}]}`))
			args := []string{"-key=offline-fixture-key", "-input=Springfield"}
			if tc.include {
				args = append(args, "-components="+tc.value)
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
			if tc.value == "" {
				if _, present := query["components"]; present {
					t.Errorf("empty components sent: %v", query)
				}
			} else if values := query["components"]; len(values) != 1 || values[0] != tc.value {
				t.Errorf("components = %v, want %s", values, tc.value)
			}
			for _, value := range []string{"Fixture Springfield", "springfield-id", "Springfield"} {
				if !strings.Contains(stdout.String(), value) {
					t.Errorf("output lacks decoded %q: %s", value, &stdout)
				}
			}
		})
	}
}

func TestComponentsRejectMalformedBeforeHTTP(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK"}`))
	for _, value := range []string{"country", "us", ":us", "country:", ":", "country:us:ca", "country:us:", "locality:Paris", "unknown:value", "|", "|country:us", "country:us|", "country:us||country:ca", "country:us|country", "country:us|country:", "country:us|unknown:value"} {
		t.Run(value, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := invoke(t, []string{"-key=fixture", "-input=Springfield", "-components=" + value}, &stdout, &stderr, options)
			if code != 2 || err == nil || !strings.Contains(err.Error(), "component") {
				t.Errorf("run = (%d, %v), want component usage error", code, err)
			}
			if len(requests) != 0 {
				t.Fatalf("invalid components reached HTTP")
			}
		})
	}
}

func TestRunOptionsAndIsolation(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK","predictions":[{"description":"Sydney Opera House","place_id":"opera-house","distance_meters":245,"structured_formatting":{"main_text":"Opera House","secondary_text":"Sydney"}}]}`))
	var previousToken string
	for i, extra := range [][]string{
		{"-location=-33.8560,151.2150", "-radius=500", "-offset=3", "-language=fr", "-types=ESTABLISHMENT", "-strictbounds", "-components=country:au|country:nz"},
		nil,
	} {
		var stdout, stderr bytes.Buffer
		code, err := invoke(t, append([]string{"-key=fixture", "-input=Opera"}, extra...), &stdout, &stderr, options)
		if code != 0 || err != nil {
			t.Fatalf("run %d = (%d, %v)", i, code, err)
		}
		if len(requests) != 1 {
			t.Fatalf("requests = %d", len(requests))
		}
		query := <-requests
		token := query.Get("sessiontoken")
		parsed, err := uuid.Parse(token)
		if err != nil || parsed == uuid.Nil || token == previousToken {
			t.Errorf("session token = %q, previous = %q", token, previousToken)
		}
		previousToken = token
		for key, want := range map[string]string{"location": "-33.856,151.215", "radius": "500", "offset": "3", "language": "fr", "types": "establishment", "strictbounds": "true", "components": "country:au|country:nz"} {
			if i == 0 {
				if query.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, query.Get(key), want)
				}
			} else if _, present := query[key]; present {
				t.Errorf("second run retained %s: %v", key, query)
			}
		}
		for _, value := range []string{"Sydney Opera House", "opera-house", "245", "Opera House", "Sydney"} {
			if !strings.Contains(stdout.String(), value) {
				t.Errorf("decoded output lacks %q: %s", value, &stdout)
			}
		}
	}
}
