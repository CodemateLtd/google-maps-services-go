// Copyright 2017 Google Inc. All Rights Reserved.
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

package maps

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// mockServerForQueryWithImage returns a mock server that only responds to a particular query string, and responds with an encoded Image.
func mockServerForQueryWithImage(query string, code int, img image.Image) *countingServer {
	server := &countingServer{}

	server.s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query != "" && r.URL.RawQuery != query {
			server.failed = append(server.failed, r.URL.RawQuery)
			http.Error(w, fmt.Sprintf("Expected '%s', got '%s'", query, r.URL.RawQuery), 999)
			return
		}
		server.successful++

		w.WriteHeader(code)
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)
	}))

	return server
}

func TestStaticMode(t *testing.T) {

	response := image.NewRGBA(image.Rect(0, 0, 640, 400))

	server := mockServerForQueryWithImage("center=Brooklyn+Bridge%2CNew+York%2CNY&format=PNG&key=AIzaNotReallyAnAPIKey&language=EN-us&maptype=roadmap&region=US&scale=2&size=600x300&zoom=13", 200, response)
	defer server.s.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))
	r := &StaticMapRequest{
		Center:   "Brooklyn Bridge,New York,NY",
		Size:     "600x300",
		Zoom:     13,
		Scale:    2,
		Language: "EN-us",
		Format:   "PNG",
		Region:   "US",
		MapType:  MapType("roadmap"),
	}

	resp, err := c.StaticMap(context.Background(), r)
	if err != nil {
		t.Errorf("r.StaticMap returned non nil error: %+v", err)
	}

	if resp.Bounds().Min.X != 0 || resp.Bounds().Min.Y != 0 || resp.Bounds().Max.X != 640 || resp.Bounds().Max.Y != 400 {
		t.Errorf("Response image not of the correct dimensions")
	}
}

func TestStaticMapPathViewport(t *testing.T) {
	response := image.NewRGBA(image.Rect(0, 0, 2, 1))
	response.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	response.SetRGBA(1, 0, color.RGBA{B: 255, A: 255})

	tests := []struct {
		name    string
		request StaticMapRequest
		query   string
	}{
		{
			name: "path only",
			request: StaticMapRequest{
				Size: "2x1",
				Paths: []Path{{
					Color:  "red",
					Weight: 3,
					Location: []LatLng{
						{Lat: 1, Lng: 2},
						{Lat: 3, Lng: 4},
					},
				}},
			},
			query: "key=AIzaNotReallyAnAPIKey&path=color%3Ared%7Cweight%3A3%7C1%2C2%7C3%2C4&size=2x1",
		},
		{
			name: "markers remain accepted",
			request: StaticMapRequest{
				Size:    "2x1",
				Markers: []Marker{{LocationAddress: "Brooklyn"}},
			},
			query: "key=AIzaNotReallyAnAPIKey&markers=Brooklyn&size=2x1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := mockServerForQueryWithImage(tt.query, http.StatusOK, response)
			defer server.s.Close()
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))
			if err != nil {
				t.Fatal(err)
			}

			got, err := c.StaticMap(context.Background(), &tt.request)
			if err != nil {
				t.Fatalf("StaticMap returned an error: %v", err)
			}
			if got.Bounds() != response.Bounds() {
				t.Fatalf("image bounds = %v, want %v", got.Bounds(), response.Bounds())
			}
			for x := 0; x < 2; x++ {
				want := response.RGBAAt(x, 0)
				if pixel := color.RGBAModel.Convert(got.At(x, 0)).(color.RGBA); pixel != want {
					t.Errorf("pixel at (%d, 0) = %v, want %v", x, pixel, want)
				}
			}
		})
	}
}

func TestStaticMapPathViewportValidation(t *testing.T) {
	var dispatched int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&dispatched, 1)
		http.Error(w, "unexpected dispatch", http.StatusBadRequest)
	}))
	defer server.Close()
	c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		request StaticMapRequest
	}{
		{
			name:    "all positioning sources absent",
			request: StaticMapRequest{Size: "2x1"},
		},
		{
			name: "path without size",
			request: StaticMapRequest{
				Paths: []Path{{
					Location: []LatLng{
						{Lat: 1, Lng: 2},
						{Lat: 3, Lng: 4},
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := c.StaticMap(context.Background(), &tt.request); err == nil || got != nil {
				t.Errorf("StaticMap = (%v, %v), want nil image and a validation error", got, err)
			}
			if got := atomic.LoadInt32(&dispatched); got != 0 {
				t.Errorf("validation dispatched %d HTTP requests, want 0", got)
			}
		})
	}
}

func TestMapStyles(t *testing.T) {
	r := StaticMapRequest{
		MapStyles: []string{
			"x", "y",
		},
	}
	values := r.params()
	if q := values.Encode(); q != "style=x&style=y" {
		t.Errorf("Generated query string is wrong: %s", q)
	}
}

func TestCustomIconMarkers(t *testing.T) {
	marker := Marker{
		CustomIcon: CustomIcon{
			IconURL: "icon@2x.png",
			Anchor:  "topleft",
			Scale:   2,
		},
		Location: []LatLng{
			{
				Lat: 3.1225951401,
				Lng: 101.6404967928,
			},
		},
	}

	r := StaticMapRequest{
		Markers: []Marker{marker},
	}

	markers := r.params().Get("markers")
	if m := markers; m != "icon:icon@2x.png|anchor:topleft|scale:2|3.1225951401,101.6404967928" {
		t.Errorf("Generated query string is wrong: %s", m)
	}
}

func TestMarkersWithLocationAndAddress(t *testing.T) {
	marker := Marker{
		Location: []LatLng{
			{
				Lat: 3.1225951401,
				Lng: 101.6404967928,
			},
		},
		LocationAddress: "my Marker address",
	}

	r := StaticMapRequest{
		Markers: []Marker{marker},
	}

	markers := r.params().Get("markers")
	if m := markers; m != "3.1225951401,101.6404967928|my Marker address" {
		t.Errorf("Generated query string is wrong: %s", m)
	}
}
