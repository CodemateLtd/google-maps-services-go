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
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

type staticCLITransport func(*http.Request) (*http.Response, error)

func (f staticCLITransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStaticCLI(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		want   url.Values
		signed bool
	}{
		{
			name: "no overlays retains viewport and image options",
			args: []string{"-key=offline-key", "-center=Paris", "-zoom=12", "-scale=2", "-format=png32", "-maptype=terrain", "-mapid=fixture-map", "-language=fr", "-region=FR"},
			want: url.Values{"key": {"offline-key"}, "center": {"Paris"}, "zoom": {"12"}, "scale": {"2"}, "format": {"png32"}, "maptype": {"terrain"}, "map_id": {"fixture-map"}, "language": {"fr"}, "region": {"FR"}},
		},
		{
			name: "repeat markers provide viewport and encode custom icon",
			args: []string{"-key=offline-key", `-marker={"color":"blue","label":"A","size":"mid","locations":[[37.4,-122.1],[38,-123]]}`, `-marker={"locations":[[37.5,-122.2]],"icon":{"url":"https://example.com/pin.png?a=1&b=2","anchor":"bottom","scale":2}}`},
			want: url.Values{"key": {"offline-key"}, "markers": {"color:blue|label:A|size:mid|37.4,-122.1|38,-123", "icon:https://example.com/pin.png?a=1&b=2|anchor:bottom|scale:2|37.5,-122.2"}},
		},
		{
			name: "pixel anchor and icon scale",
			args: []string{"-key=offline-key", `-marker={"locations":[[37.4,-122.1],[38,-123]],"icon":{"url":"https://example.com/pin.png?a=1&b=2","anchor":"12,24","scale":4}}`},
			want: url.Values{"key": {"offline-key"}, "markers": {"icon:https://example.com/pin.png?a=1&b=2|anchor:12,24|scale:4|37.4,-122.1|38,-123"}},
		},
		{
			name: "repeat paths visible points and styles",
			args: []string{"-key=offline-key", "-center=0,0", "-zoom=3", `-path={"color":"0xff0000ff","weight":3,"geodesic":true,"locations":[[0,0],[1,1]]}`, `-path={"fill_color":"0x0000ff40","locations":[[38.5,-120.2],[40.7,-120.95],[43.252,-126.453]]}`, "-visible=0,0", "-visible=1,1", "-style=feature:water|element:geometry|color:0x0000ff", "-style=feature:road|visibility:off"},
			want: url.Values{"key": {"offline-key"}, "center": {"0,0"}, "zoom": {"3"}, "path": {"color:0xff0000ff|weight:3|geodesic:true|0,0|1,1", "fillcolor:0x0000ff40|enc:_p~iF~ps|U_ulLnnqC_mqNvxq`@"}, "visible": {"0,0|1,1"}, "style": {"feature:water|element:geometry|color:0x0000ff", "feature:road|visibility:off"}},
		},
		{
			name: "retained API key signing",
			args: []string{"-key=offline-key", "-client_id=ignored-legacy-client", "-signature=c2VjcmV0", "-center=Paris", "-zoom=12"},
			want: url.Values{"key": {"offline-key"}, "center": {"Paris"}, "zoom": {"12"}}, signed: true,
		},
		{
			name: "retained eligible legacy client signing",
			args: []string{"-client_id=gme-fixture", "-signature=c2VjcmV0", "-center=Paris", "-zoom=12"},
			want: url.Values{"client": {"gme-fixture"}, "center": {"Paris"}, "zoom": {"12"}}, signed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, code, err, requests := runStaticCLI(t, append([]string{"-size=2x3"}, tt.args...))
			if code != 0 || err != nil {
				t.Fatalf("run = (%d, %v); output: %s", code, err, output)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %d, want 1", len(requests))
			}
			want := tt.want
			want.Set("size", "2x3")
			if tt.signed {
				mac := hmac.New(sha1.New, []byte("secret"))
				mac.Write([]byte("/maps/api/staticmap?" + want.Encode()))
				want.Set("signature", base64.URLEncoding.EncodeToString(mac.Sum(nil)))
			}
			if !reflect.DeepEqual(requests[0], want) {
				t.Errorf("encoded SDK query = %#v, want %#v", requests[0], want)
			}
			assertStaticImageOutput(t, output)
			for _, credential := range []string{"offline-key", "c2VjcmV0", "gme-fixture"} {
				if strings.Contains(output, credential) {
					t.Errorf("output disclosed credential %q", credential)
				}
			}
		})
	}
}

func TestStaticCLIRejectsMalformedOverlaysBeforeHTTP(t *testing.T) {
	tests := []struct{ name, arg, diagnostic string }{
		{"bad JSON", `-marker={`, "invalid overlay JSON"},
		{"unknown field", `-marker={"location":[[0,0]]}`, "unknown field"},
		{"extra JSON", `-marker={"locations":[[0,0]]} {}`, "expected one JSON object"},
		{"trailing closing token", `-marker={"locations":[[0,0]]}}`, "expected one JSON object"},
		{"empty marker", `-marker={}`, "at least one location"},
		{"short coordinate", `-marker={"locations":[[0]]}`, "exactly latitude and longitude"},
		{"long coordinate", `-marker={"locations":[[0,0,0]]}`, "exactly latitude and longitude"},
		{"null coordinate", `-marker={"locations":[[null,0]]}`, "must be numbers"},
		{"object coordinate", `-marker={"locations":[{"lat":0,"lng":0}]}`, "coordinate must be"},
		{"string coordinate", `-marker={"locations":[["0",0]]}`, "coordinate must be"},
		{"latitude range", `-marker={"locations":[[91,0]]}`, "latitude in"},
		{"longitude range", `-path={"locations":[[0,0],[0,-181]]}`, "longitude in"},
		{"overflow coordinate", `-marker={"locations":[[1e999,0]]}`, "coordinate must be"},
		{"empty path", `-path={}`, "at least two locations"},
		{"short path", `-path={"locations":[[0,0]]}`, "at least two locations"},
		{"missing icon URL", `-marker={"locations":[[0,0]],"icon":{"scale":2}}`, "icon URL"},
		{"relative icon URL", `-marker={"locations":[[0,0]],"icon":{"url":"pin.png"}}`, "icon URL"},
		{"bad URL escape", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/%zz"}}`, "icon URL"},
		{"non HTTP icon URL", `-marker={"locations":[[0,0]],"icon":{"url":"ftp://example.com/pin.png"}}`, "icon URL"},
		{"icon credentials", `-marker={"locations":[[0,0]],"icon":{"url":"https://user:password@example.com/pin.png"}}`, "icon URL"},
		{"icon pipe", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/pin|scale:9"}}`, "icon URL"},
		{"invalid icon anchor", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/pin.png","anchor":"sideways"}}`, "icon anchor"},
		{"invalid pixel anchor", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/pin.png","anchor":"1,2,3"}}`, "icon anchor"},
		{"negative pixel anchor", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/pin.png","anchor":"-1,2"}}`, "icon anchor"},
		{"invalid icon scale", `-marker={"locations":[[0,0]],"icon":{"url":"https://example.com/pin.png","scale":3}}`, "icon scale"},
		{"mixed icon and marker options", `-marker={"color":"blue","locations":[[0,0]],"icon":{"url":"https://example.com/pin.png"}}`, "cannot be combined"},
		{"visible cardinality", "-visible=0,0,0", "latitude,longitude"},
		{"visible nonnumeric", "-visible=west,0", "numeric"},
		{"visible nonfinite", "-visible=NaN,0", "finite"},
		{"visible infinity", "-visible=0,+Inf", "finite"},
		{"visible range", "-visible=-91,0", "latitude in"},
		{"empty style", "-style= ", "style must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, code, err, requests := runStaticCLI(t, []string{"-key=offline-key", "-size=2x3", "-center=0,0", "-zoom=3", tt.arg})
			if code != 2 || err != nil {
				t.Errorf("run = (%d, %v), want (2, nil); output: %s", code, err, output)
			}
			if len(requests) != 0 {
				t.Errorf("malformed input reached HTTP: %#v", requests)
			}
			if !strings.Contains(output, tt.diagnostic) {
				t.Errorf("output = %s, want diagnostic %q", output, tt.diagnostic)
			}
		})
	}
}

func TestStaticCLIHelp(t *testing.T) {
	output, code, err, requests := runStaticCLI(t, []string{"-h"})
	if code != 0 || err != nil || len(requests) != 0 {
		t.Fatalf("help = (%d, %v), requests = %#v; output: %s", code, err, requests, output)
	}
}

// A literal 2-by-3 RGB PNG whose six pixels are all (17, 34, 51).
const staticPNG = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAADCAIAAAA2iEnWAAAAEElEQVR4nGMQVDIGIgYUCgAYYwJlZzrqoAAAAABJRU5ErkJggg=="

type staticFixture struct {
	mu       sync.Mutex
	requests []url.Values
	options  []maps.ClientOption
}

func newStaticFixture(t *testing.T, status int, body []byte) *staticFixture {
	t.Helper()
	fixture := &staticFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, r.URL.Query())
		fixture.mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/staticmap" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(status)
		if _, err := w.Write(body); err != nil {
			t.Errorf("writing fixture response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	base := client.Transport
	client.Timeout = 3 * time.Second
	client.Transport = staticCLITransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme+"://"+r.URL.Host != server.URL {
			return nil, fmt.Errorf("unexpected nonfixture request: %s", r.URL)
		}
		return base.RoundTrip(r)
	})
	fixture.options = []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}
	return fixture
}

func (f *staticFixture) recorded() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.requests...)
}

func staticImageBytes(t *testing.T) []byte {
	t.Helper()
	body, err := base64.StdEncoding.DecodeString(staticPNG)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func runStaticCLI(t *testing.T, args []string) (string, int, error, []url.Values) {
	t.Helper()
	fixture := newStaticFixture(t, http.StatusOK, staticImageBytes(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code, err := run(ctx, args, &stdout, &stderr, fixture.options...)
	return stdout.String() + stderr.String(), code, err, fixture.recorded()
}

func assertStaticImageOutput(t *testing.T, output string) {
	t.Helper()
	// Inspect decoded image data without depending on pretty's layout.
	compact := strings.Join(strings.Fields(output), "")
	for _, field := range []string{"image.RGBA", "Stride:8", "Max:image.Point{X:2,Y:3}"} {
		if !strings.Contains(compact, field) {
			t.Errorf("decoded image output lacks %q: %s", field, output)
		}
	}
	for _, component := range []string{"0x11", "0x22", "0x33", "0xff"} {
		if count := strings.Count(output, component); count != 6 {
			t.Errorf("decoded component %s count = %d, want 6 pixels; output: %s", component, count, output)
		}
	}
}

func TestStaticRunInputFailures(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantError bool
	}{
		{"missing credentials", nil, true},
		{"incomplete legacy client", []string{"-client_id=gme-fixture"}, true},
		{"secret without identity", []string{"-signature=c2VjcmV0"}, true},
		{"invalid key secret", []string{"-key=offline-key", "-signature=%%%"}, true},
		{"invalid legacy secret", []string{"-client_id=gme-fixture", "-signature=%%%"}, true},
		{"unknown flag", []string{"-key=offline-key", "-unknown=true"}, false},
		{"nonnumeric zoom", []string{"-key=offline-key", "-zoom=near"}, false},
		{"nonnumeric scale", []string{"-key=offline-key", "-scale=large"}, false},
		{"unexpected positional argument", []string{"-key=offline-key", "-size=2x3", "-center=Paris", "-zoom=12", "extra"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newStaticFixture(t, http.StatusOK, staticImageBytes(t))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			code, err := run(ctx, tt.args, &stdout, &stderr, fixture.options...)
			if code != 2 || (err != nil) != tt.wantError {
				t.Errorf("run = (%d, %v), want usage failure (returned error: %t)", code, err, tt.wantError)
			}
			if requests := fixture.recorded(); len(requests) != 0 {
				t.Errorf("invalid input dispatched HTTP: %#v", requests)
			}
			if stdout.Len() != 0 {
				t.Errorf("invalid input wrote success output: %s", &stdout)
			}
		})
	}
}

func TestStaticRunIsolation(t *testing.T) {
	for _, failedFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("first_run_parse_failure_%t", failedFirst), func(t *testing.T) {
			fixture := newStaticFixture(t, http.StatusOK, staticImageBytes(t))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first := []string{"-key=first-key", "-center=Paris", "-zoom=12", "-size=2x3", "-format=png32", "-scale=2",
				`-marker={"locations":[[0,0]]}`, `-path={"locations":[[0,0],[1,1]]}`,
				"-visible=1,1", "-style=feature:road|visibility:off"}
			wantFirstCode, wantRequests := 0, 2
			if failedFirst {
				first = append(first, "-visible=bad")
				wantFirstCode, wantRequests = 2, 1
			}
			var stdout, stderr bytes.Buffer
			code, err := run(ctx, first, &stdout, &stderr, fixture.options...)
			if code != wantFirstCode || err != nil {
				t.Fatalf("first run = (%d, %v), want (%d, nil)", code, err, wantFirstCode)
			}
			stdout.Reset()
			stderr.Reset()
			code, err = run(ctx, []string{"-key=second-key", "-center=Tokyo", "-zoom=8", "-size=2x3"}, &stdout, &stderr, fixture.options...)
			if code != 0 || err != nil {
				t.Fatalf("second run = (%d, %v); stderr: %s", code, err, &stderr)
			}
			if stderr.Len() != 0 {
				t.Errorf("successful run wrote diagnostics: %s", &stderr)
			}
			assertStaticImageOutput(t, stdout.String())
			requests := fixture.recorded()
			if len(requests) != wantRequests {
				t.Fatalf("requests = %#v, want %d", requests, wantRequests)
			}
			want := url.Values{"key": {"second-key"}, "center": {"Tokyo"}, "zoom": {"8"}, "size": {"2x3"}}
			if got := requests[len(requests)-1]; !reflect.DeepEqual(got, want) {
				t.Errorf("second invocation retained flags: %#v, want %#v", got, want)
			}
		})
	}
}

func TestStaticRunSDKErrors(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         []byte
		args         []string
		want         string
		wantRequests int
	}{
		{"HTTP rejection", http.StatusForbidden, []byte("fixture denied this map"), []string{"-size=2x3"}, "Maps Static API: 403 - fixture denied this map", 1},
		{"invalid image", http.StatusOK, []byte("not a PNG"), []string{"-size=2x3"}, "image: unknown format", 1},
		{"missing size", http.StatusOK, nil, nil, "maps: Size empty", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newStaticFixture(t, tt.status, tt.body)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			args := append([]string{"-key=offline-key", "-center=Paris", "-zoom=12"}, tt.args...)
			code, err := run(ctx, args, &stdout, &stderr, fixture.options...)
			if code != 1 || err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("run = (%d, %v), want service failure containing %q", code, err, tt.want)
			}
			if requests := fixture.recorded(); len(requests) != tt.wantRequests {
				t.Errorf("requests = %#v, want %d", requests, tt.wantRequests)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("SDK error should return to caller, stdout = %q, stderr = %q", &stdout, &stderr)
			}
		})
	}
}

type staticErrorWriter struct{ err error }

func (w staticErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStaticRunWriterError(t *testing.T) {
	fixture := newStaticFixture(t, http.StatusOK, staticImageBytes(t))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	want := errors.New("fixture output unavailable")
	var stderr bytes.Buffer
	code, err := run(ctx, []string{"-key=offline-key", "-center=Paris", "-zoom=12", "-size=2x3"}, staticErrorWriter{want}, &stderr, fixture.options...)
	if code != 1 || !errors.Is(err, want) {
		t.Errorf("run = (%d, %v), want output failure %v", code, err, want)
	}
	if requests := fixture.recorded(); len(requests) != 1 {
		t.Errorf("requests = %#v, want one decoded response before writing", requests)
	}
	if stderr.Len() != 0 {
		t.Errorf("output error should return to caller, stderr = %q", &stderr)
	}
}

func TestStaticRunCanceledContext(t *testing.T) {
	fixture := newStaticFixture(t, http.StatusOK, staticImageBytes(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	code, err := run(ctx, []string{"-key=offline-key", "-center=Paris", "-zoom=12", "-size=2x3"}, &stdout, &stderr, fixture.options...)
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Errorf("run = (%d, %v), want canceled service error", code, err)
	}
	if requests := fixture.recorded(); len(requests) != 0 {
		t.Errorf("canceled invocation dispatched HTTP: %#v", requests)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("cancellation wrote output: stdout = %q, stderr = %q", &stdout, &stderr)
	}
}
