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

const elevationFixture = `{"status":"OK","results":[{"location":{"lat":38.5,"lng":-120.2},"elevation":123.75,"resolution":4.5},{"location":{"lat":0,"lng":0},"elevation":0,"resolution":0}]}`

func elevationServer(t *testing.T, body string) (*httptest.Server, chan url.Values) {
	t.Helper()
	queries := make(chan url.Values, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/elevation/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		queries <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server, queries
}

type elevationLocalTransport func(*http.Request) (*http.Response, error)

func (f elevationLocalTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func invokeElevation(t *testing.T, server *httptest.Server, args []string, stdout io.Writer) (int, error, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	client := *server.Client()
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = elevationLocalTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != server.URL {
			return nil, fmt.Errorf("offline fixture blocked destination %s", r.URL)
		}
		return base.RoundTrip(r)
	})
	code, err := run(ctx, args, stdout, &stderr, maps.WithBaseURL(server.URL), maps.WithHTTPClient(&client))
	return code, err, stderr.String()
}

func TestElevationInputsAndDecodedOutput(t *testing.T) {
	const encoded = "enc:_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	for _, tt := range []struct {
		name                     string
		args                     []string
		locations, path, samples string
	}{
		{"single-zero", []string{"-locations=0,0"}, "enc:??", "", ""},
		{"delimited-locations", []string{"-locations=38.5,-120.2|40.7,-120.95|43.252,-126.453"}, encoded, "", ""},
		{"encoded-locations", []string{"-locations=" + encoded}, encoded, "", ""},
		{"delimited-path", []string{"-path=38.5,-120.2|40.7,-120.95|43.252,-126.453", "-samples=7"}, "", encoded, "7"},
		{"encoded-path", []string{"-path=" + encoded, "-samples=3"}, "", encoded, "3"},
		{"nonpositive-samples-with-locations", []string{"-locations=0,0", "-samples=-2"}, "enc:??", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, queries := elevationServer(t, elevationFixture)
			var stdout bytes.Buffer
			code, err, stderr := invokeElevation(t, server, append([]string{"-key=fixture"}, tt.args...), &stdout)
			if code != 0 || err != nil || stderr != "" {
				t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
			}
			if len(queries) != 1 {
				t.Fatalf("requests = %d, want 1", len(queries))
			}
			q := <-queries
			for key, want := range map[string]string{"locations": tt.locations, "path": tt.path, "samples": tt.samples, "key": "fixture"} {
				if q.Get(key) != want {
					t.Errorf("%s = %q, want %q", key, q.Get(key), want)
				}
			}
			for _, pattern := range []string{`Elevation:\s*123\.75`, `Resolution:\s*4\.5`, `Lat:\s*38\.5`, `Lng:\s*-120\.2`, `Elevation:\s*0[,\s}]`, `Resolution:\s*0[,\s}]`} {
				if !regexp.MustCompile(pattern).MatchString(stdout.String()) {
					t.Errorf("missing decoded field %s: %s", pattern, stdout.String())
				}
			}
		})
	}
}

func TestElevationMalformedInputsAndHelp(t *testing.T) {
	server, queries := elevationServer(t, elevationFixture)
	cases := [][]string{
		{"-unknown"}, {"-key=fixture", "-samples=no"}, {"-key=fixture"}, {"-locations=0,0"},
		{"-client_id=legacy", "-locations=0,0"}, {"-signature=c2VjcmV0", "-locations=0,0"},
		{"-client_id=legacy", "-signature=!", "-locations=0,0"},
		{"-key=fixture", "-path=0,0|1,1"}, {"-key=fixture", "-path=0,0|1,1", "-samples=-1"},
		{"-key=fixture", "-locations=0,0", "extra"},
	}
	for _, flag := range []string{"locations", "path"} {
		for _, input := range []string{"1", "1,2,3", ",2", "1,", "bad,2", "1,bad", "1,2|3", "1,2|", "|1,2", "enc:"} {
			cases = append(cases, []string{"-key=fixture", "-" + flag + "=" + input, "-samples=2"})
		}
	}
	for _, args := range cases {
		var stdout bytes.Buffer
		code, _, _ := invokeElevation(t, server, args, &stdout)
		if code != 2 || stdout.Len() != 0 {
			t.Errorf("run(%v) = %d, stdout %q", args, code, stdout.String())
		}
	}
	var stdout bytes.Buffer
	code, err, _ := invokeElevation(t, server, []string{"-help"}, &stdout)
	if code != 0 || err != nil || stdout.Len() != 0 {
		t.Errorf("help = (%d, %v), stdout %q", code, err, stdout.String())
	}
	if len(queries) != 0 {
		t.Errorf("invalid/help made %d requests", len(queries))
	}
}

type elevationBrokenWriter struct{}

func (elevationBrokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("fixture-output-failed")
}

func TestElevationFailureBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		broken           bool
	}{
		{"service", `{"status":"REQUEST_DENIED","error_message":"fixture-elevation-denied"}`, "fixture-elevation-denied", false},
		{"decode", `{"status":"OK","results":[`, "unexpected", false},
		{"output", elevationFixture, "fixture-output-failed", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, queries := elevationServer(t, tt.body)
			var buffer bytes.Buffer
			var stdout io.Writer = &buffer
			if tt.broken {
				stdout = elevationBrokenWriter{}
			}
			code, err, _ := invokeElevation(t, server, []string{"-key=fixture", "-locations=0,0"}, stdout)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tt.want) || len(queries) != 1 || buffer.Len() != 0 {
				t.Fatalf("run = (%d, %v), requests %d, stdout %q", code, err, len(queries), buffer.String())
			}
		})
	}
}

func TestElevationRepeatedRunAndCredentials(t *testing.T) {
	server, queries := elevationServer(t, elevationFixture)
	for _, args := range [][]string{
		{"-key=preferred", "-client_id=legacy", "-signature=!", "-path=0,0|1,1", "-samples=2"},
		{"-client_id=legacy", "-signature=c2VjcmV0", "-locations=0,0"},
	} {
		var stdout bytes.Buffer
		code, err, _ := invokeElevation(t, server, args, &stdout)
		if code != 0 || err != nil {
			t.Fatalf("run = (%d, %v)", code, err)
		}
	}
	if len(queries) != 2 {
		t.Fatalf("requests = %d, want 2", len(queries))
	}
	first, second := <-queries, <-queries
	if first.Get("key") != "preferred" || first.Get("client") != "" || first.Get("samples") != "2" {
		t.Errorf("first request = %v", first)
	}
	if second.Get("client") != "legacy" || second.Get("key") != "" || second.Get("locations") != "enc:??" {
		t.Errorf("second request = %v", second)
	}
	for _, key := range []string{"path", "samples"} {
		if _, ok := second[key]; ok {
			t.Errorf("second run retained %s: %v", key, second)
		}
	}
}
