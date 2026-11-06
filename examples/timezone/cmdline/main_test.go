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
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

const timezoneFixture = `{"status":"OK","dstOffset":3600,"rawOffset":-28800,"timeZoneId":"America/Los_Angeles","timeZoneName":"Pacific Daylight Time"}`

func timezoneServer(t *testing.T, body string) (*httptest.Server, chan url.Values) {
	t.Helper()
	queries := make(chan url.Values, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/timezone/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server, queries
}

type timezoneLocalTransport func(*http.Request) (*http.Response, error)

func (f timezoneLocalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func invokeTimezone(t *testing.T, server *httptest.Server, args []string, stdout io.Writer) (int, error, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	client := *server.Client()
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = timezoneLocalTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != server.URL {
			return nil, fmt.Errorf("offline fixture blocked destination %s", r.URL)
		}
		return base.RoundTrip(r)
	})
	code, err := run(ctx, args, stdout, &stderr, maps.WithBaseURL(server.URL), maps.WithHTTPClient(&client))
	return code, err, stderr.String()
}

func TestTimezoneCoordinatesTimestampAndOutput(t *testing.T) {
	for _, tt := range []struct{ name, location, timestamp, wantTimestamp string }{
		{"unix-epoch-and-zero-coordinates", "0,0", "0", "0"},
		{"signed-and-padded-seconds", "-33.86,151.2", "+001335892800", "1335892800"},
		{"before-epoch", "38.5,-120.2", "-1", "-1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, queries := timezoneServer(t, timezoneFixture)
			var stdout bytes.Buffer
			code, err, stderr := invokeTimezone(t, server, []string{"-key=fixture", "-location=" + tt.location, "-timestamp=" + tt.timestamp, "-language=fr"}, &stdout)
			if code != 0 || err != nil || stderr != "" {
				t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
			}
			if len(queries) != 1 {
				t.Fatalf("requests = %d, want 1", len(queries))
			}
			q := <-queries
			for key, want := range map[string]string{"key": "fixture", "location": tt.location, "timestamp": tt.wantTimestamp, "language": "fr"} {
				if q.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, q.Get(key), want)
				}
			}
			for _, pattern := range []string{`DstOffset:\s*3600`, `RawOffset:\s*-28800`, `TimeZoneID:\s*"America/Los_Angeles"`, `TimeZoneName:\s*"Pacific Daylight Time"`} {
				if !regexp.MustCompile(pattern).MatchString(stdout.String()) {
					t.Errorf("missing decoded field %s: %s", pattern, stdout.String())
				}
			}
		})
	}
}

func TestTimezoneMalformedInputsAndHelp(t *testing.T) {
	server, queries := timezoneServer(t, timezoneFixture)
	cases := [][]string{
		{"-unknown"}, {"-key=fixture", "-location=0,0"}, {"-key=fixture", "-timestamp=0"},
		{"-location=0,0", "-timestamp=0"}, {"-client_id=legacy", "-location=0,0", "-timestamp=0"},
		{"-signature=c2VjcmV0", "-location=0,0", "-timestamp=0"},
		{"-client_id=legacy", "-signature=!", "-location=0,0", "-timestamp=0"},
		{"-key=fixture", "-location=0,0", "-timestamp=0", "extra"},
	}
	for _, input := range []string{"1", "1,2,3", ",2", "1,", ",", "bad,2", "1,bad", "1|2"} {
		cases = append(cases, []string{"-key=fixture", "-location=" + input, "-timestamp=0"})
	}
	for _, input := range []string{"", "now", "1.5", "9223372036854775808"} {
		cases = append(cases, []string{"-key=fixture", "-location=0,0", "-timestamp=" + input})
	}
	for _, args := range cases {
		var stdout bytes.Buffer
		code, _, _ := invokeTimezone(t, server, args, &stdout)
		if code != 2 || stdout.Len() != 0 {
			t.Errorf("run(%v) = %d, stdout %q", args, code, stdout.String())
		}
	}
	var stdout bytes.Buffer
	code, err, _ := invokeTimezone(t, server, []string{"-help"}, &stdout)
	if code != 0 || err != nil || stdout.Len() != 0 {
		t.Errorf("help = (%d, %v), stdout %q", code, err, stdout.String())
	}
	if len(queries) != 0 {
		t.Errorf("invalid/help made %d requests", len(queries))
	}
}

type timezoneBrokenWriter struct{}

func (timezoneBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("fixture-output-failed") }

func TestTimezoneFailureBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		broken           bool
	}{
		{"service", `{"status":"REQUEST_DENIED","error_message":"fixture-timezone-denied"}`, "fixture-timezone-denied", false},
		{"decode", `{"status":"OK","rawOffset":`, "unexpected", false},
		{"output", timezoneFixture, "fixture-output-failed", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, queries := timezoneServer(t, tt.body)
			var buffer bytes.Buffer
			var stdout io.Writer = &buffer
			if tt.broken {
				stdout = timezoneBrokenWriter{}
			}
			code, err, _ := invokeTimezone(t, server, []string{"-key=fixture", "-location=0,0", "-timestamp=0"}, stdout)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tt.want) || len(queries) != 1 || buffer.Len() != 0 {
				t.Fatalf("run = (%d, %v), requests %d, stdout %q", code, err, len(queries), buffer.String())
			}
		})
	}
}

func TestTimezoneRepeatedRunAndCredentials(t *testing.T) {
	server, queries := timezoneServer(t, `{"status":"OK","dstOffset":0,"rawOffset":0,"timeZoneId":"Etc/UTC","timeZoneName":"Coordinated Universal Time"}`)
	for _, args := range [][]string{
		{"-key=preferred", "-client_id=legacy", "-signature=!", "-location=38.5,-120.2", "-timestamp=1335892800", "-language=fr"},
		{"-client_id=legacy", "-signature=c2VjcmV0", "-location=0,0", "-timestamp=0"},
	} {
		var stdout bytes.Buffer
		code, err, _ := invokeTimezone(t, server, args, &stdout)
		if code != 0 || err != nil {
			t.Fatalf("run = (%d, %v)", code, err)
		}
		for _, pattern := range []string{`DstOffset:\s*0[,\s}]`, `RawOffset:\s*0[,\s}]`, `TimeZoneID:\s*"Etc/UTC"`} {
			if !regexp.MustCompile(pattern).MatchString(stdout.String()) {
				t.Errorf("missing zero-offset output %s: %s", pattern, stdout.String())
			}
		}
	}
	if len(queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(queries))
	}
	first, second := <-queries, <-queries
	if first.Get("key") != "preferred" || first.Get("client") != "" {
		t.Errorf("key precedence lost: %v", first)
	}
	if second.Get("client") != "legacy" || second.Get("key") != "" || second.Get("location") != "0,0" || second.Get("timestamp") != "0" {
		t.Errorf("second request = %v", second)
	}
	if _, ok := second["language"]; ok {
		t.Errorf("second run retained language: %v", second)
	}
}
