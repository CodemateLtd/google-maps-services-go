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
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunValidationAndHelp(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK"}`))
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"help", []string{"-help"}, 0},
		{"unknown flag", []string{"-not_a_flag"}, 2},
		{"missing input", []string{"-key=fixture"}, 2},
		{"bad location", []string{"-key=fixture", "-input=pizza", "-location=1,2,3"}, 2},
		{"missing longitude", []string{"-key=fixture", "-input=pizza", "-location=1"}, 2},
		{"nonnumeric coordinate", []string{"-key=fixture", "-input=pizza", "-location=1,west"}, 2},
		{"negative radius", []string{"-key=fixture", "-input=pizza", "-radius=-1"}, 2},
		{"bad offset", []string{"-key=fixture", "-input=pizza", "-offset=word"}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code, err := invoke(t, tc.args, &stdout, &stderr, options)
			if code != tc.code {
				t.Errorf("run = (%d, %v), want code %d", code, err, tc.code)
			}
			if tc.code == 0 && err != nil {
				t.Errorf("help error = %v", err)
			}
			if len(requests) != 0 {
				t.Errorf("input/help reached HTTP")
			}
			if stdout.Len() != 0 {
				t.Errorf("unexpected result output: %s", &stdout)
			}
		})
	}
}

func TestRunServiceAndOutputErrors(t *testing.T) {
	for _, tc := range []struct{ name, body, message string }{
		{"service status", `{"status":"REQUEST_DENIED","error_message":"fixture permission denied"}`, "fixture permission denied"},
		{"bad response", `{"status":`, "unexpected EOF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options, requests := fixture(t, http.StatusOK, "application/json", []byte(tc.body))
			var stdout, stderr bytes.Buffer
			code, err := invoke(t, []string{"-key=fixture", "-input=pizza"}, &stdout, &stderr, options)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("run = (%d, %v), want %q", code, err, tc.message)
			}
			if len(requests) != 1 {
				t.Errorf("requests = %d, want 1", len(requests))
			}
			if stdout.Len() != 0 {
				t.Errorf("failed service produced result: %s", &stdout)
			}
		})
	}
	t.Run("output failure", func(t *testing.T) {
		options, requests := fixture(t, http.StatusOK, "application/json", []byte(`{"status":"OK","predictions":[{"description":"Pizza in Sydney"}]}`))
		outputErr := errors.New("fixture output failure")
		var stderr bytes.Buffer
		code, err := invoke(t, []string{"-key=fixture", "-input=pizza"}, failingWriter{outputErr}, &stderr, options)
		if code != 1 || !errors.Is(err, outputErr) {
			t.Errorf("run = (%d, %v), want output error", code, err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %d, want 1", len(requests))
		}
	})
}
