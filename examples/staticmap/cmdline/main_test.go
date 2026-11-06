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
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type staticCLITransport func(*http.Request) (*http.Response, error)

func (f staticCLITransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise main's real flag parsing and exit behavior while routing all HTTP locally.
func TestStaticCLIProcess(t *testing.T) {
	fixture := os.Getenv("GO_STATIC_CLI_FIXTURE")
	if fixture == "" {
		return
	}
	target, err := url.Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("GO_STATIC_CLI_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	base := http.DefaultTransport
	http.DefaultTransport = staticCLITransport(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return base.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = base }()
	os.Args = append([]string{os.Args[0]}, args...)
	main()
}

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
			args: []string{"-key=offline-key", "-signature=c2VjcmV0", "-center=Paris", "-zoom=12"},
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
			output, err, requests := runStaticCLI(t, append([]string{"-size=2x3"}, tt.args...))
			if err != nil {
				t.Fatalf("CLI failed: %v; output: %s", err, output)
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
			if !strings.Contains(output, "image.RGBA") {
				t.Errorf("output does not contain the decoded PNG image: %s", output)
			}
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
			output, err, requests := runStaticCLI(t, []string{"-key=offline-key", "-size=2x3", "-center=0,0", "-zoom=3", tt.arg})
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
				t.Errorf("exit = %v, want usage exit 2; output: %s", err, output)
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
	output, err, requests := runStaticCLI(t, []string{"-h"})
	if err != nil || len(requests) != 0 {
		t.Fatalf("help error = %v, requests = %#v; output: %s", err, requests, output)
	}
}

func runStaticCLI(t *testing.T, args []string) (string, error, []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Query())
		mu.Unlock()
		if r.Method != http.MethodGet || r.URL.Path != "/maps/api/staticmap" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		img := image.NewRGBA(image.Rect(0, 0, 2, 3))
		for y := 0; y < 3; y++ {
			for x := 0; x < 2; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 17, G: 34, B: 51, A: 255})
			}
		}
		w.Header().Set("Content-Type", "image/png")
		if err := png.Encode(w, img); err != nil {
			t.Errorf("encoding PNG fixture: %v", err)
		}
	}))
	defer server.Close()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStaticCLIProcess$")
	cmd.Env = append(os.Environ(), "GO_STATIC_CLI_FIXTURE="+server.URL, "GO_STATIC_CLI_ARGS="+string(encoded))
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("CLI timed out: %v; output: %s", ctx.Err(), output)
	}
	mu.Lock()
	defer mu.Unlock()
	return string(output), err, append([]url.Values(nil), requests...)
}
