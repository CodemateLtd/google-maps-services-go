// Copyright 2015 Google Inc. All Rights Reserved.
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
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestClientChannelIsConfigured(t *testing.T) {
	_, err := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithChannel("Test-Channel"))
	if err != nil {
		t.Errorf("Unable to create client with channel")
	}
}

func TestClientWithExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, err := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithExperienceId(ids...))
	assert.Nil(t, err)
	assert.Equal(t, c.experienceId, ids)
}

func TestClientSetExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.setExperienceId(ids...)
	assert.Equal(t, c.experienceId, ids)
}

func TestClientGetExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.experienceId = ids
	assert.Equal(t, c.getExperienceId(), ids)
}

func TestClientClearExperienceId(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	c.experienceId = ids
	c.clearExperienceId()
	assert.Nil(t, c.experienceId)
}

func TestClientSetExperienceIdHeader(t *testing.T) {
	ids := []string{"foo", "bar"}
	c, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"))

	// slice has two elements
	c.experienceId = ids
	req, _ := http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))

	// slice is nil
	c.experienceId = nil
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, "", req.Header.Get(ExperienceIdHeaderName))

	// slice is empty
	c.experienceId = []string{}
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(context.Background(), req)
	assert.Equal(t, "", req.Header.Get(ExperienceIdHeaderName))

	var ctx context.Context
	// context has one element
	c.experienceId = []string{}
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, "foo")
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, "foo", req.Header.Get(ExperienceIdHeaderName))

	// context has two elements
	c.experienceId = []string{}
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, ids...)
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))

	// context has two elements and client has two elements
	c.experienceId = ids
	ctx = context.Background()
	ctx = ExperienceIdContext(ctx, ids...)
	req, _ = http.NewRequest("GET", "/", nil)
	c.setExperienceIdHeader(ctx, req)
	assert.Equal(t, strings.Join(ids, ",") + "," + strings.Join(ids, ","), req.Header.Get(ExperienceIdHeaderName))
}

func TestClientExperienceIdSample(t *testing.T) {
	// [START maps_experience_id]
	experienceId := uuid.New().String()

	// instantiate client with experience id
	client, _ := NewClient(WithAPIKey("AIza-Maps-API-Key"), WithExperienceId("foo"))

	// clear the current experience id
	client.clearExperienceId()

	// set a new experience id
	otherExperienceId := uuid.New().String()
	client.setExperienceId(experienceId, otherExperienceId)

	// make API request, the client will set the header
	// X-GOOG-MAPS-EXPERIENCE-ID: experienceId,otherExperienceId

	// get current experience id
	var ids []string
	ids = client.getExperienceId()
	// [END maps_experience_id]

	assert.Equal(t, ids, []string{experienceId, otherExperienceId})
}

func TestClientHTTPTimeout(t *testing.T) {
	geocode := func(ctx context.Context, c *Client) error {
		_, err := c.Geocode(ctx, &GeocodingRequest{Address: "Sydney"})
		return err
	}
	geolocate := func(ctx context.Context, c *Client) error {
		_, err := c.Geolocate(ctx, &GeolocationRequest{ConsiderIP: true})
		return err
	}
	photo := func(ctx context.Context, c *Client) error {
		response, err := c.PlacePhoto(ctx, &PlacePhotoRequest{PhotoReference: "photo", MaxWidth: 1})
		if err != nil {
			return err
		}
		defer response.Data.Close()
		data, err := ioutil.ReadAll(response.Data)
		if string(data) != "photo-prefix" {
			return fmt.Errorf("photo prefix = %q, want %q", data, "photo-prefix")
		}
		return err
	}
	tests := []struct {
		name   string
		prefix string
		call   func(context.Context, *Client) error
	}{
		{"GET headers", "", geocode},
		{"GET body", `{"results":[`, geocode},
		{"POST headers", "", geolocate},
		{"POST body", `{"location":`, geolocate},
		{"photo headers", "", photo},
		{"photo body", "photo-prefix", photo},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.prefix != "" {
					fmt.Fprint(w, tt.prefix)
					w.(http.Flusher).Flush()
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL),
				WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err = tt.call(ctx, c)
			if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
				t.Fatalf("request error = %v, want HTTP timeout", err)
			}
			if ctx.Err() != nil {
				t.Fatalf("harness expired instead of the HTTP timeout: %v", ctx.Err())
			}
		})
	}
}

func TestClientContextCancellation(t *testing.T) {
	for _, clientType := range []string{"default", "custom", "disabled"} {
		for _, cancellation := range []string{"deadline", "cancel"} {
			clientType, cancellation := clientType, cancellation
			t.Run(clientType+"/"+cancellation, func(t *testing.T) {
				t.Parallel()
				started := make(chan struct{})
				release := make(chan struct{})
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					close(started)
					select {
					case <-r.Context().Done():
					case <-release:
					}
				}))
				defer server.Close()
				defer close(release)
				options := []ClientOption{WithAPIKey(apiKey), WithBaseURL(server.URL)}
				switch clientType {
				case "custom":
					options = append(options, WithHTTPClient(&http.Client{Timeout: 2 * time.Second}))
				case "disabled":
					options = append(options, WithHTTPClient(&http.Client{Timeout: 0}))
				}
				c, err := NewClient(options...)
				if err != nil {
					t.Fatal(err)
				}
				var ctx context.Context
				var cancel context.CancelFunc
				want := context.Canceled
				if cancellation == "deadline" {
					ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
					want = context.DeadlineExceeded
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				result := make(chan error, 1)
				go func() {
					_, err := c.Geocode(ctx, &GeocodingRequest{Address: "Sydney"})
					result <- err
				}()
				guard := time.NewTimer(time.Second)
				defer guard.Stop()
				if cancellation == "cancel" {
					select {
					case <-started:
						cancel()
					case <-guard.C:
						t.Fatal("request did not reach the server")
					}
				}
				select {
				case err := <-result:
					if err == nil || ctx.Err() != want {
						t.Fatalf("request error = %v, context error = %v, want %v", err, ctx.Err(), want)
					}
				case <-guard.C:
					t.Fatal("request outlived caller cancellation")
				}
			})
		}
	}
}

func TestClientPhotoStreamWithCustomTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, time.Minute} {
		timeout := timeout
		t.Run(timeout.String(), func(t *testing.T) {
			t.Parallel()
			resume := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				fmt.Fprint(w, "photo-prefix")
				w.(http.Flusher).Flush()
				select {
				case <-resume:
					fmt.Fprint(w, "-suffix")
				case <-r.Context().Done():
				}
			}))
			defer server.Close()
			defer close(resume)
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL),
				WithHTTPClient(&http.Client{Timeout: timeout}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			response, err := c.PlacePhoto(ctx, &PlacePhotoRequest{PhotoReference: "photo", MaxWidth: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Data.Close()
			prefix := make([]byte, len("photo-prefix"))
			if _, err := io.ReadFull(response.Data, prefix); err != nil {
				t.Fatal(err)
			}
			resume <- struct{}{}
			suffix, err := ioutil.ReadAll(response.Data)
			if err != nil || string(prefix)+string(suffix) != "photo-prefix-suffix" {
				t.Fatalf("photo data = %q%q, error = %v", prefix, suffix, err)
			}
		})
	}
}
