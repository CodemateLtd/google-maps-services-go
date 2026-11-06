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
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

type jpegOutput struct {
	data       bytes.Buffer
	writeErr   error
	closeErr   error
	closeCalls int
}

func (o *jpegOutput) Write(p []byte) (int, error) {
	if o.writeErr != nil {
		return 0, o.writeErr
	}
	return o.data.Write(p)
}

func (o *jpegOutput) Close() error {
	o.closeCalls++
	return o.closeErr
}

func TestWriteJPEGFile(t *testing.T) {
	filename := filepath.Join(os.TempDir(), "maps-photo-"+uuid.New().String()+".jpg")
	file, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := file.Stat(); err == nil {
			file.Close()
		}
		os.Remove(filename)
	}()

	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 220, G: 30, B: 10, A: 255})
		}
	}
	if err := writeJPEG(file, img); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); err == nil {
		t.Fatal("JPEG output file is still open")
	}
	input, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	decoded, err := jpeg.Decode(input)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Fatalf("JPEG bounds = %v, want %v", decoded.Bounds(), img.Bounds())
	}
	pixel := color.NRGBAModel.Convert(decoded.At(1, 1)).(color.NRGBA)
	if pixel.R < 210 || pixel.R > 230 || pixel.G < 20 || pixel.G > 40 || pixel.B > 20 {
		t.Errorf("JPEG pixel = %v, want approximately (220, 30, 10)", pixel)
	}
}

func TestWriteJPEGFailures(t *testing.T) {
	writeErr := errors.New("output write failed")
	closeErr := errors.New("output close failed")
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	tests := []struct {
		name          string
		image         image.Image
		writeErr      error
		closeErr      error
		want          error
		encodeFailure bool
	}{
		{"write failure", img, writeErr, nil, writeErr, false},
		{"close failure", img, nil, closeErr, closeErr, false},
		{"write failure takes precedence", img, writeErr, closeErr, writeErr, false},
		// A uniform image's unbounded dimensions cannot be encoded as a JPEG.
		{"encode failure takes precedence", image.NewUniform(color.Black), nil, closeErr, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &jpegOutput{writeErr: tt.writeErr, closeErr: tt.closeErr}
			err := writeJPEG(output, tt.image)
			if tt.encodeFailure {
				if err == nil || errors.Is(err, closeErr) {
					t.Errorf("error = %v, want the primary JPEG encoding failure", err)
				}
			} else if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
			if output.closeCalls != 1 {
				t.Errorf("close count = %d, want 1", output.closeCalls)
			}
		})
	}
}
