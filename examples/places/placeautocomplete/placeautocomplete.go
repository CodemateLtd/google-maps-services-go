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

// Package main contains a simple command line tool for Places API Query Autocomplete
// Documentation: https://developers.google.com/places/web-service/query
//
// Authentication requires -key. The former -client_id and -signature flags
// were removed because Places does not support Premium Plan client IDs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/kr/pretty"
	"googlemaps.github.io/maps"
)

var (
	apiKey       = flag.String("key", "", "API Key for using Google Maps API.")
	input        = flag.String("input", "", "Text string on which to search.")
	language     = flag.String("language", "", "The language in which to return results.")
	offset       = flag.Uint("offset", 0, "The character position in the input term at which the service uses text for predictions.")
	location     = flag.String("location", "", "The latitude/longitude around which to retrieve place information. This must be specified as latitude,longitude.")
	radius       = flag.Uint("radius", 0, "Defines the distance (in meters) within which to bias place results. The maximum allowed radius is 50,000 meters.")
	placeType    = flag.String("types", "", "Restricts the results to places matching the specified type.")
	components   = flag.String("components", "", "Country filters separated by |, for example 'country:us|country:ca'.")
	strictbounds = flag.Bool("strictbounds", false, "Whether to strictly enforce bounds.")
)

func usageAndExit(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	fmt.Println("Flags:")
	flag.PrintDefaults()
	os.Exit(2)
}

func check(err error) {
	if err != nil {
		log.Fatalf("fatal error: %s", err)
	}
}

func main() {
	flag.Parse()

	var client *maps.Client
	var err error
	if *apiKey == "" {
		usageAndExit("Please specify an API Key.")
	}
	client, err = maps.NewClient(maps.WithAPIKey(*apiKey))
	check(err)

	// s is a session token. Thread this through a series of requests that comprise
	// a single autocomplete search session.
	s := maps.NewPlaceAutocompleteSessionToken()

	r := &maps.PlaceAutocompleteRequest{
		Input:        *input,
		Language:     *language,
		Offset:       *offset,
		Radius:       *radius,
		StrictBounds: *strictbounds,
		SessionToken: s,
	}

	parseLocation(*location, r)
	parsePlaceType(*placeType, r)
	check(parseComponents(*components, r))

	resp, err := client.PlaceAutocomplete(context.Background(), r)
	check(err)

	pretty.Println(resp)
}

func parseLocation(location string, r *maps.PlaceAutocompleteRequest) {
	if location != "" {
		l, err := maps.ParseLatLng(location)
		check(err)
		r.Location = &l
	}
}

func parsePlaceType(placeType string, r *maps.PlaceAutocompleteRequest) {
	if placeType != "" {
		t, err := maps.ParseAutocompletePlaceType(placeType)
		check(err)
		r.Types = t
	}
}

func parseComponents(components string, r *maps.PlaceAutocompleteRequest) error {
	if components == "" {
		return nil
	}
	for _, component := range strings.Split(components, "|") {
		parts := strings.SplitN(component, ":", 3)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("invalid component %q: expected name:value", component)
		}
		switch parts[0] {
		case "country":
			if r.Components == nil {
				r.Components = make(map[maps.Component][]string)
			}
			r.Components[maps.ComponentCountry] = append(r.Components[maps.ComponentCountry], parts[1])
		default:
			return fmt.Errorf("unsupported component %q", parts[0])
		}
	}
	return nil
}
