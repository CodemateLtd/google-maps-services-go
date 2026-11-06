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
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"googlemaps.github.io/maps"
)

// Independent 3x2 red JPEG, not produced by the encoder under test.
const photoFixtureBase64 = "/9j/4AAQSkZJRgABAQAASABIAAD/4QBMRXhpZgAATU0AKgAAAAgAAYdpAAQAAAABAAAAGgAAAAAAA6ABAAMAAAABAAEAAKACAAQAAAABAAAAA6ADAAQAAAABAAAAAgAAAAD/7QA4UGhvdG9zaG9wIDMuMAA4QklNBAQAAAAAAAA4QklNBCUAAAAAABDUHYzZjwCyBOmACZjs+EJ+/8AAEQgAAgADAwEiAAIRAQMRAf/EAB8AAAEFAQEBAQEBAAAAAAAAAAABAgMEBQYHCAkKC//EALUQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+v/EAB8BAAMBAQEBAQEBAQEAAAAAAAABAgMEBQYHCAkKC//EALURAAIBAgQEAwQHBQQEAAECdwABAgMRBAUhMQYSQVEHYXETIjKBCBRCkaGxwQkjM1LwFWJy0QoWJDThJfEXGBkaJicoKSo1Njc4OTpDREVGR0hJSlNUVVZXWFlaY2RlZmdoaWpzdHV2d3h5eoKDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uLj5OXm5+jp6vLz9PX29/j5+v/bAEMAAgICAgICAwICAwUDAwMFBgUFBQUGCAYGBgYGCAoICAgICAgKCgoKCgoKCgwMDAwMDA4ODg4ODw8PDw8PDw8PD//bAEMBAgICBAQEBwQEBxALCQsQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEP/dAAQAAf/aAAwDAQACEQMRAD8A+U6KKK/lc/2EP//Z"

func photoFixture(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(photoFixtureBase64)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func photoDirectory(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(os.TempDir(), "maps-photo-command-"+uuid.New().String())
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func TestRunPhotoDecodesAndWritesOptionalJPEG(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "image/jpeg", photoFixture(t))
	dir := photoDirectory(t)
	basename := filepath.Join(dir, "download")
	for i, extra := range [][]string{
		{"-maxheight=2", "-maxwidth=3", "-basename=" + basename},
		{"-maxwidth=3"},
		{"-maxheight=2"},
	} {
		var stdout, stderr bytes.Buffer
		code, err := invoke(t, append([]string{"-key=fixture", "-photoreference=red-photo"}, extra...), &stdout, &stderr, options)
		if code != 0 || err != nil {
			t.Fatalf("run %d = (%d, %v)", i, code, err)
		}
		if len(requests) != 1 {
			t.Fatalf("requests = %d, want 1", len(requests))
		}
		query := <-requests
		if query.Get("photoreference") != "red-photo" {
			t.Errorf("reference = %q", query.Get("photoreference"))
		}
		wantWidth, wantHeight := "3", "2"
		if i == 1 {
			wantHeight = ""
		}
		if i == 2 {
			wantWidth = ""
		}
		if query.Get("maxwidth") != wantWidth || query.Get("maxheight") != wantHeight {
			t.Errorf("dimensions = %v", query)
		}
		if stdout.Len() != 0 {
			t.Errorf("photo unexpectedly wrote image data to stdout: %q", &stdout)
		}
		for _, value := range []string{"Content-Type: image/jpeg", "Image bounds: (0,0)-(3,2)"} {
			if !strings.Contains(stderr.String(), value) {
				t.Errorf("metadata lacks %q: %s", value, &stderr)
			}
		}
		if i == 0 {
			if !strings.Contains(stderr.String(), "Wrote image to "+basename+".jpg") {
				t.Errorf("missing closed-file success: %s", &stderr)
			}
			file, err := os.Open(basename + ".jpg")
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(file)
			closeErr := file.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			if decoded.Bounds() != image.Rect(0, 0, 3, 2) {
				t.Errorf("JPEG bounds = %v", decoded.Bounds())
			}
			pixel := color.NRGBAModel.Convert(decoded.At(1, 1)).(color.NRGBA)
			if pixel.R < 205 || pixel.R > 235 || pixel.G < 15 || pixel.G > 45 || pixel.B > 30 {
				t.Errorf("JPEG pixel = %v, want red fixture", pixel)
			}
			// A later invocation must not retain the basename or recreate this file.
			if err := os.Remove(basename + ".jpg"); err != nil {
				t.Fatal(err)
			}
		} else {
			if strings.Contains(stderr.String(), "Wrote image") {
				t.Errorf("omitted basename reported a write: %s", &stderr)
			}
			if _, err := os.Stat(basename + ".jpg"); !os.IsNotExist(err) {
				t.Errorf("omitted basename created output: %v", err)
			}
		}
	}
}

func TestRunPhotoValidationAndHelp(t *testing.T) {
	options, requests := fixture(t, http.StatusOK, "image/jpeg", photoFixture(t))
	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"help", []string{"-help"}, 0},
		{"unknown flag", []string{"-unknown"}, 2},
		{"missing reference", []string{"-key=fixture", "-maxwidth=3"}, 2},
		{"missing dimensions", []string{"-key=fixture", "-photoreference=red-photo"}, 2},
		{"negative width", []string{"-key=fixture", "-photoreference=red-photo", "-maxwidth=-1"}, 2},
		{"negative height with width", []string{"-key=fixture", "-photoreference=red-photo", "-maxheight=-1", "-maxwidth=3"}, 2},
		{"malformed dimension", []string{"-key=fixture", "-photoreference=red-photo", "-maxheight=invalid"}, 2},
	} {
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
				t.Errorf("invalid input/help reached HTTP")
			}
		})
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunPhotoServiceAndOutputErrors(t *testing.T) {
	args := []string{"-key=fixture", "-photoreference=red-photo", "-maxwidth=3"}
	t.Run("HTTP denial", func(t *testing.T) {
		options, requests := fixture(t, http.StatusForbidden, "application/json", []byte(`{"error":{"code":403,"message":"fixture photo permission denied"}}`))
		var stdout, stderr bytes.Buffer
		code, err := invoke(t, args, &stdout, &stderr, options)
		var httpErr *maps.HTTPError
		if code != 1 || !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusForbidden || !strings.Contains(err.Error(), "fixture photo permission denied") {
			t.Errorf("run = (%d, %v), want HTTP 403", code, err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %d", len(requests))
		}
		if strings.Contains(stderr.String(), "Wrote image") {
			t.Errorf("HTTP failure reported success: %s", &stderr)
		}
	})
	t.Run("invalid image", func(t *testing.T) {
		options, requests := fixture(t, http.StatusOK, "image/jpeg", []byte("not an image"))
		var stdout, stderr bytes.Buffer
		basename := filepath.Join(photoDirectory(t), "bad-image")
		code, err := invoke(t, append(append([]string(nil), args...), "-basename="+basename), &stdout, &stderr, options)
		if code != 1 || err == nil || !strings.Contains(err.Error(), "unknown format") {
			t.Errorf("run = (%d, %v), want decode error", code, err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %d", len(requests))
		}
		if _, err := os.Stat(basename + ".jpg"); !os.IsNotExist(err) {
			t.Errorf("decode failure created output: %v", err)
		}
		if strings.Contains(stderr.String(), "Image bounds") || strings.Contains(stderr.String(), "Wrote image") {
			t.Errorf("decode failure reported success: %s", &stderr)
		}
	})
	t.Run("cannot create output", func(t *testing.T) {
		options, requests := fixture(t, http.StatusOK, "image/jpeg", photoFixture(t))
		var stdout, stderr bytes.Buffer
		basename := filepath.Join(photoDirectory(t), "missing", "photo")
		code, err := invoke(t, append(append([]string(nil), args...), "-basename="+basename), &stdout, &stderr, options)
		var pathErr *os.PathError
		if code != 1 || !errors.As(err, &pathErr) {
			t.Errorf("run = (%d, %v), want filesystem error", code, err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %d", len(requests))
		}
		if strings.Contains(stderr.String(), "Wrote image") {
			t.Errorf("create failure reported success: %s", &stderr)
		}
	})
	t.Run("metadata output failure", func(t *testing.T) {
		options, requests := fixture(t, http.StatusOK, "image/jpeg", photoFixture(t))
		var stdout bytes.Buffer
		outputErr := errors.New("fixture metadata writer failed")
		code, err := invoke(t, args, &stdout, failingWriter{outputErr}, options)
		if code != 1 || !errors.Is(err, outputErr) {
			t.Errorf("run = (%d, %v), want output error", code, err)
		}
		if len(requests) != 1 {
			t.Errorf("requests = %d", len(requests))
		}
	})
}
