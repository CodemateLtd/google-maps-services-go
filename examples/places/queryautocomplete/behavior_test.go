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

func TestRunPredictionOptionsAndIsolation(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK","predictions":[{"description":"Pizza near Sydney Harbour","place_id":"harbour-pizza","matched_substrings":[{"length":5,"offset":0}],"terms":[{"offset":0,"value":"Pizza"},{"offset":11,"value":"Sydney Harbour"}],"structured_formatting":{"main_text":"Pizza","secondary_text":"Sydney Harbour"}}]}`))
	for i, extra := range [][]string{
		{"-location=-33.8560,151.2150", "-radius=250", "-offset=5", "-language=en"},
		nil,
	} {
		var stdout, stderr bytes.Buffer
		code, err := invoke(t, append([]string{"-key=fixture", "-input=pizza near Syd"}, extra...), &stdout, &stderr, options)
		if code != 0 || err != nil {
			t.Fatalf("run = (%d, %v)", code, err)
		}
		if len(requests) != 1 {
			t.Fatalf("requests = %d, want 1", len(requests))
		}
		query := <-requests
		if _, present := query["sessiontoken"]; present {
			t.Errorf("query autocomplete invented session token: %v", query)
		}
		for key, want := range map[string]string{"location": "-33.856,151.215", "radius": "250", "offset": "5", "language": "en"} {
			if i == 0 {
				if query.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, query.Get(key), want)
				}
			} else if _, present := query[key]; present {
				t.Errorf("second run retained %s: %v", key, query)
			}
		}
		for _, value := range []string{"Pizza near Sydney Harbour", "harbour-pizza", "Sydney Harbour", "Offset:", "11", "Length:", "5"} {
			if !strings.Contains(stdout.String(), value) {
				t.Errorf("output lacks decoded %q: %s", value, &stdout)
			}
		}
	}
}
