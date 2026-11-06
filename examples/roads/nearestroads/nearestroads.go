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

// Package main demonstrates Roads API Nearest Roads for independent points,
// not an interpolated path. Enable Roads API in a billing-enabled Google Cloud
// project and supply that project's API key.
// See https://developers.google.com/maps/documentation/roads/nearest.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"strings"

	"googlemaps.github.io/maps"
)

var (
	apiKey = flag.String("key", "", "API key for a billing-enabled project with Roads API enabled.")
	points = flag.String("points", "", "Independent latitude,longitude pairs separated by | (quote the argument). Latitude must be in [-90,90], longitude in [-180,180].")
)

func usageAndExit(message string) {
	fmt.Fprintln(os.Stderr, message)
	flag.Usage()
	os.Exit(2)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: nearestroads -key API_KEY -points 'latitude,longitude|latitude,longitude'")
		fmt.Fprintln(os.Stderr, "Find nearest roads for independent points, not an interpolated path.")
		fmt.Fprintln(os.Stderr, "Enable Roads API and billing in the key's Google Cloud project.")
		fmt.Fprintln(os.Stderr, "Results include zero-based original_index values into the supplied points; absent indices are marked absent.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 0 {
		usageAndExit("Unexpected positional arguments.")
	}
	if strings.TrimSpace(*apiKey) == "" {
		usageAndExit("Please specify an API key with -key.")
	}
	parsed, err := parsePoints(*points)
	if err != nil {
		usageAndExit(err.Error())
	}
	client, err := maps.NewClient(maps.WithAPIKey(*apiKey))
	check(err)
	response, err := client.NearestRoads(context.Background(), &maps.NearestRoadsRequest{Points: parsed})
	check(err)
	for _, point := range response.SnappedPoints {
		fmt.Printf("location=%s place_id=%q original_index=", point.Location.String(), point.PlaceID)
		if point.OriginalIndex == nil {
			fmt.Println("absent")
		} else {
			fmt.Println(*point.OriginalIndex)
		}
	}
}

func parsePoints(value string) ([]maps.LatLng, error) {
	if strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("Please specify independent points with -points.")
	}
	pairs := strings.Split(value, "|")
	result := make([]maps.LatLng, len(pairs))
	for i, pair := range pairs {
		// ParseLatLng assumes two fields and ignores extras, so check shape first.
		if strings.Count(pair, ",") != 1 {
			return nil, fmt.Errorf("Point %d must be a latitude,longitude pair.", i)
		}
		point, err := maps.ParseLatLng(pair)
		if err != nil {
			return nil, fmt.Errorf("Point %d must contain two numeric coordinates.", i)
		}
		if math.IsNaN(point.Lat) || math.IsInf(point.Lat, 0) || math.IsNaN(point.Lng) || math.IsInf(point.Lng, 0) {
			return nil, fmt.Errorf("Point %d coordinates must be finite.", i)
		}
		if point.Lat < -90 || point.Lat > 90 || point.Lng < -180 || point.Lng > 180 {
			return nil, fmt.Errorf("Point %d latitude must be in [-90,90] and longitude in [-180,180].", i)
		}
		result[i] = point
	}
	return result, nil
}

func check(err error) {
	if err == nil {
		return
	}
	// net/http wraps failures with the complete request URL, including the key.
	if requestError, ok := err.(*url.Error); ok {
		err = requestError.Err
	}
	fmt.Fprintf(os.Stderr, "Nearest Roads failed: %s\n", err)
	os.Exit(1)
}
