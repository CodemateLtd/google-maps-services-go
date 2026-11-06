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

// Package main contains a simple command line tool for DistanceMatrix
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
	flags := flag.NewFlagSet("distancematrix", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API key for Google Maps API access (recommended).")
	clientID := flags.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature := flags.String("signature", "", "URL signing secret for the eligible existing customer's legacy client ID.")
	origins := flags.String("origins", "", "Origin addresses or coordinates, separated with the pipe (|) character.")
	destinations := flags.String("destinations", "", "Destination addresses or coordinates, separated with the pipe (|) character.")
	mode := flags.String("mode", "", "The mode of transport to use when calculating distance.")
	language := flags.String("language", "", "The language in which to return results.")
	avoid := flags.String("avoid", "", "The route restriction.")
	units := flags.String("units", "", "The unit system to use when expressing distance as text.")
	departureTime := flags.String("departure_time", "", "The desired time of departure.")
	arrivalTime := flags.String("arrival_time", "", "The desired time of arrival.")
	transitMode := flags.String("transit_mode", "", "Preferred modes of transit, | separated.")
	transitRoutingPreference := flags.String("transit_routing_preference", "", "Preferences for transit requests.")
	trafficModel := flags.String("traffic_model", "", "Assumptions to use when calculating time in traffic.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if flags.NArg() != 0 {
		return 2, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *origins == "" || *destinations == "" {
		return 2, fmt.Errorf("origins and destinations are required")
	}
	r := &maps.DistanceMatrixRequest{
		Origins: strings.Split(*origins, "|"), Destinations: strings.Split(*destinations, "|"),
		Language: *language, DepartureTime: *departureTime, ArrivalTime: *arrivalTime,
	}
	switch *mode {
	case "", "driving", "walking", "bicycling", "transit":
		r.Mode = maps.Mode(*mode)
	default:
		return 2, fmt.Errorf("unknown mode %q", *mode)
	}
	switch *avoid {
	case "", "tolls", "highways", "ferries":
		r.Avoid = maps.Avoid(*avoid)
	default:
		return 2, fmt.Errorf("unknown avoid restriction %q", *avoid)
	}
	switch *units {
	case "", "metric", "imperial":
		r.Units = maps.Units(*units)
	default:
		return 2, fmt.Errorf("unknown units %q", *units)
	}
	if *transitMode != "" {
		for _, value := range strings.Split(*transitMode, "|") {
			switch value {
			case "bus", "subway", "train", "tram", "rail":
				r.TransitMode = append(r.TransitMode, maps.TransitMode(value))
			default:
				return 2, fmt.Errorf("unknown transit mode %q", value)
			}
		}
	}
	switch *transitRoutingPreference {
	case "", "fewer_transfers", "less_walking":
		r.TransitRoutingPreference = maps.TransitRoutingPreference(*transitRoutingPreference)
	default:
		return 2, fmt.Errorf("unknown transit routing preference %q", *transitRoutingPreference)
	}
	switch *trafficModel {
	case "", "best_guess", "pessimistic", "optimistic":
		r.TrafficModel = maps.TrafficModel(*trafficModel)
	default:
		return 2, fmt.Errorf("unknown traffic model %q", *trafficModel)
	}
	if r.DepartureTime != "" && r.ArrivalTime != "" {
		return 2, fmt.Errorf("departure_time and arrival_time cannot both be specified")
	}
	if (len(r.TransitMode) != 0 || r.TransitRoutingPreference != "") && r.Mode != maps.TravelModeTransit {
		return 2, fmt.Errorf("transit options require transit mode")
	}
	if r.Mode == maps.TravelModeTransit && r.TrafficModel != "" {
		return 2, fmt.Errorf("cannot specify transit mode and traffic model together")
	}
	var options []maps.ClientOption
	if *apiKey != "" {
		options = append(options, maps.WithAPIKey(*apiKey))
	} else if *clientID != "" || *signature != "" {
		options = append(options, maps.WithClientIDAndSignature(*clientID, *signature))
	} else {
		return 2, fmt.Errorf("please specify an API key, or an eligible existing customer's legacy client ID and URL signing secret")
	}
	client, err := maps.NewClient(append(options, clientOptions...)...)
	if err != nil {
		return 2, err
	}
	resp, err := client.DistanceMatrix(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}
