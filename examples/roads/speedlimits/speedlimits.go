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

// Package main contains a simple command line tool for the Roads Speed Limits API.
// Docs: https://developers.google.com/maps/documentation/roads/speed-limits
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
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
	flags := flag.NewFlagSet("speedlimits", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API Key for using Google Maps API.")
	path := flags.String("path", "", "The path to be snapped. Separate latitude/longitude pairs with pipes and latitude and longitude with commas.")
	placeIDs := flags.String("place_ids", "", "The place IDs of road segments, separated by commas.")
	units := flags.String("units", "", "Speed units: KPH or MPH. Defaults to KPH.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if flags.NArg() != 0 {
		return 2, fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *apiKey == "" {
		return 2, fmt.Errorf("please specify an API key")
	}
	r := &maps.SpeedLimitsRequest{}
	switch *units {
	case "":
	case "KPH":
		r.Units = maps.SpeedLimitKPH
	case "MPH":
		r.Units = maps.SpeedLimitMPH
	default:
		return 2, fmt.Errorf("units must be KPH or MPH")
	}
	if *path == "" && *placeIDs == "" {
		return 2, fmt.Errorf("please specify a path or a list of place IDs")
	}
	if *path != "" {
		points, err := parsePath(*path)
		if err != nil {
			return 2, err
		}
		r.Path = points
	}
	if *placeIDs != "" {
		r.PlaceID = strings.Split(*placeIDs, ",")
		for _, id := range r.PlaceID {
			if strings.TrimSpace(id) == "" {
				return 2, fmt.Errorf("place IDs must not be empty")
			}
		}
	}
	options := append([]maps.ClientOption{maps.WithAPIKey(*apiKey)}, clientOptions...)
	client, err := maps.NewClient(options...)
	if err != nil {
		return 1, err
	}
	resp, err := client.SpeedLimits(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

// parsePath validates the shape before calling the SDK's coordinate parser.
func parsePath(path string) ([]maps.LatLng, error) {
	locations := strings.Split(path, "|")
	points := make([]maps.LatLng, 0, len(locations))
	for i, location := range locations {
		if strings.Count(location, ",") != 1 {
			return nil, fmt.Errorf("path point %d must contain latitude,longitude", i+1)
		}
		point, err := maps.ParseLatLng(location)
		if err != nil {
			return nil, fmt.Errorf("path point %d: %w", i+1, err)
		}
		if math.IsNaN(point.Lat) || math.IsInf(point.Lat, 0) || math.IsNaN(point.Lng) || math.IsInf(point.Lng, 0) {
			return nil, fmt.Errorf("path point %d must contain finite coordinates", i+1)
		}
		points = append(points, point)
	}
	return points, nil
}
