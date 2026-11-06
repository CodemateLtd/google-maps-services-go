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

// Package main contains a simple command line tool for Places Photos API
// Documentation: https://developers.google.com/places/web-service/photos
//
// Authentication requires -key. The former -client_id and -signature flags
// were removed because Places does not support Premium Plan client IDs.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"

	"googlemaps.github.io/maps"
)

func main() {
	code, err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, clientOptions ...maps.ClientOption) (int, error) {
	flags := flag.NewFlagSet("photo", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API Key for using Google Maps API.")
	photoreference := flags.String("photoreference", "", "Textual identifier that uniquely identifies a place photo.")
	maxheight := flags.Int("maxheight", 0, "Specifies the maximum desired height, in pixels, of the image returned by the Place Photos service. One of maxheight and maxwidth is required.")
	maxwidth := flags.Int("maxwidth", 0, "Specifies the maximum desired width, in pixels, of the image returned by the Place Photos service. One of maxheight and maxwidth is required.")
	basename := flags.String("basename", "", "Base name of JPEG output file; success is reported after the file is closed. If omitted, no file is written.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if *apiKey == "" {
		return 2, fmt.Errorf("Please specify an API Key.")
	}
	if *photoreference == "" {
		return 2, fmt.Errorf("Please specify a photo reference.")
	}
	if *maxheight < 0 || *maxwidth < 0 || (*maxheight == 0 && *maxwidth == 0) {
		return 2, fmt.Errorf("Please specify a positive maxheight or maxwidth; dimensions cannot be negative.")
	}
	r := &maps.PlacePhotoRequest{
		PhotoReference: *photoreference, MaxHeight: uint(*maxheight), MaxWidth: uint(*maxwidth),
	}
	options := make([]maps.ClientOption, 0, len(clientOptions)+1)
	options = append(options, clientOptions...)
	options = append(options, maps.WithAPIKey(*apiKey))
	client, err := maps.NewClient(options...)
	if err != nil {
		return 1, err
	}
	resp, err := client.PlacePhoto(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := fmt.Fprintf(stderr, "Content-Type: %v\n", resp.ContentType); err != nil {
		resp.Data.Close()
		return 1, err
	}
	// Image consumes and closes the response stream, including on decode errors.
	img, err := resp.Image()
	if err != nil {
		return 1, err
	}
	if _, err := fmt.Fprintf(stderr, "Image bounds: %v\n", img.Bounds()); err != nil {
		return 1, err
	}
	if *basename != "" {
		filename := fmt.Sprintf("%s.jpg", *basename)
		f, err := os.Create(filename)
		if err != nil {
			return 1, err
		}
		if err := writeJPEG(f, img); err != nil {
			return 1, err
		}
		if _, err := fmt.Fprintf(stderr, "Wrote image to %s\n", filename); err != nil {
			return 1, err
		}
	}
	return 0, nil
}

// writeJPEG owns out and closes it before returning, preserving an encode error
// if closing also fails.
func writeJPEG(out io.WriteCloser, img image.Image) error {
	encodeErr := jpeg.Encode(out, img, &jpeg.Options{Quality: 85})
	closeErr := out.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}
