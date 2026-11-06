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

// Package main contains a simple command line tool for Elevation API
// Directions docs: https://developers.google.com/maps/documentation/distancematrix/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/kr/pretty"
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
	flags := flag.NewFlagSet("elevation", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API key for Google Maps API access (recommended).")
	clientID := flags.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature := flags.String("signature", "", "URL signing secret for the eligible existing customer's legacy client ID.")
	locations := flags.String("locations", "", "Locations as latitude,longitude pairs separated by |, or an enc: polyline.")
	path := flags.String("path", "", "Path as latitude,longitude pairs separated by |, or an enc: polyline.")
	samples := flags.Int("samples", 0, "Number of sample points along a path.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if flags.NArg() != 0 {
		return 2, fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	options := make([]maps.ClientOption, 0, len(clientOptions)+1)
	if *apiKey != "" {
		options = append(options, maps.WithAPIKey(*apiKey))
	} else if *clientID != "" || *signature != "" {
		options = append(options, maps.WithClientIDAndSignature(*clientID, *signature))
	} else {
		return 2, fmt.Errorf("please specify an API key (recommended), or an eligible existing customer's legacy client ID and URL signing secret")
	}
	client, err := maps.NewClient(append(options, clientOptions...)...)
	if err != nil {
		return 2, err
	}
	r := &maps.ElevationRequest{}
	if *samples > 0 {
		r.Samples = *samples
	}
	if *locations != "" {
		r.Locations, err = decodeLocations(*locations)
		if err != nil {
			return 2, fmt.Errorf("locations: %w", err)
		}
	}
	if *path != "" {
		r.Path, err = decodePath(*path)
		if err != nil {
			return 2, fmt.Errorf("path: %w", err)
		}
	}
	if len(r.Locations) == 0 && len(r.Path) == 0 {
		return 2, fmt.Errorf("locations or path is required")
	}
	if len(r.Path) > 0 && r.Samples == 0 {
		return 2, fmt.Errorf("positive samples is required with path")
	}
	resp, err := client.Elevation(ctx, r)
	if err != nil {
		return 1, fmt.Errorf("could not request elevations: %w", err)
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil

}

// decodeLocations takes a location argument string and decodes it.
// This argument has three different forms, as per documentation at
// https://developers.google.com/maps/documentation/elevation/#Locations
func decodeLocations(location string) ([]maps.LatLng, error) {
	if strings.HasPrefix(location, "enc:") {
		return maps.DecodePolyline(location[len("enc:"):])
	}
	parts := strings.Split(location, "|")
	result := make([]maps.LatLng, 0, len(parts))
	for _, part := range parts {
		if strings.Count(part, ",") != 1 {
			return nil, fmt.Errorf("expected latitude,longitude, got %q", part)
		}
		point, err := maps.ParseLatLng(part)
		if err != nil {
			return nil, err
		}
		result = append(result, point)
	}
	return result, nil
}

// decodePath takes a location argument string and decodes it.
// This argument has two different forms, as per documentation at
// https://developers.google.com/maps/documentation/elevation/#Paths
func decodePath(path string) ([]maps.LatLng, error) {
	if strings.HasPrefix(path, "enc:") || strings.Contains(path, "|") {
		return decodeLocations(path)
	}
	return nil, fmt.Errorf("invalid path argument: %q", path)
}
