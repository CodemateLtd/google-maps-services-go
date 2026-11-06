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

// Package main contains a simple command line tool for Directions API
// Directions docs: https://developers.google.com/maps/documentation/directions/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kr/pretty"
	"googlemaps.github.io/maps"
)

func main() {
	code, err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	// Batch failures are already reported on stdout and retain the batch's
	// successful exit status. Other errors are diagnosed only here.
	if err != nil && code != 0 {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, clientOptions ...maps.ClientOption) (int, error) {
	flags := flag.NewFlagSet("directions", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API key for Google Maps API access (recommended).")
	clientID := flags.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature := flags.String("signature", "", "URL signing secret for the eligible existing customer's legacy client ID.")
	origin := flags.String("origin", "", "The address or textual latitude/longitude value from which you wish to calculate directions.")
	destination := flags.String("destination", "", "The address or textual latitude/longitude value to which you wish to calculate directions.")
	mode := flags.String("mode", "", "The travel mode for this directions request.")
	departureTime := flags.String("departure_time", "", "The departure time for this directions request.")
	arrivalTime := flags.String("arrival_time", "", "The arrival time for this directions request.")
	waypoints := flags.String("waypoints", "", "The waypoints for driving directions request, | separated.")
	alternatives := flags.Bool("alternatives", false, "Whether to provide more than one route alternative.")
	avoid := flags.String("avoid", "", "Features to avoid, | separated.")
	language := flags.String("language", "", "The language in which to return results.")
	units := flags.String("units", "", "The unit system to use when returning results.")
	region := flags.String("region", "", "The region code as a two-character ccTLD.")
	transitMode := flags.String("transit_mode", "", "Preferred modes of transit, | separated.")
	transitRoutingPreference := flags.String("transit_routing_preference", "", "Preferences for transit routes.")
	iterations := flags.Int("iterations", 1, "Number of API requests. Values above 1 run concurrently and report each failure.")
	trafficModel := flags.String("traffic_model", "", "Traffic prediction model: optimistic, best_guess, or pessimistic.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if flags.NArg() != 0 {
		return 2, fmt.Errorf("unexpected arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *origin == "" || *destination == "" {
		return 2, fmt.Errorf("origin and destination are required")
	}
	if *iterations < 1 {
		return 2, fmt.Errorf("iterations must be positive")
	}
	r := &maps.DirectionsRequest{
		Origin: *origin, Destination: *destination,
		DepartureTime: *departureTime, ArrivalTime: *arrivalTime,
		Alternatives: *alternatives, Language: *language, Region: *region,
	}
	switch *mode {
	case "", "driving", "walking", "bicycling", "transit":
		r.Mode = maps.Mode(*mode)
	default:
		return 2, fmt.Errorf("unknown mode %q", *mode)
	}
	switch *units {
	case "", "metric", "imperial":
		r.Units = maps.Units(*units)
	default:
		return 2, fmt.Errorf("unknown units %q", *units)
	}
	switch *transitRoutingPreference {
	case "", "fewer_transfers", "less_walking":
		r.TransitRoutingPreference = maps.TransitRoutingPreference(*transitRoutingPreference)
	default:
		return 2, fmt.Errorf("unknown transit routing preference %q", *transitRoutingPreference)
	}
	switch *trafficModel {
	case "", "optimistic", "best_guess", "pessimistic":
		r.TrafficModel = maps.TrafficModel(*trafficModel)
	default:
		return 2, fmt.Errorf("unknown traffic model %q", *trafficModel)
	}
	if *waypoints != "" {
		r.Waypoints = strings.Split(*waypoints, "|")
	}
	if *avoid != "" {
		for _, value := range strings.Split(*avoid, "|") {
			switch value {
			case "tolls", "highways", "ferries":
				r.Avoid = append(r.Avoid, maps.Avoid(value))
			default:
				return 2, fmt.Errorf("unknown avoid restriction %q", value)
			}
		}
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
	if r.DepartureTime != "" && r.ArrivalTime != "" {
		return 2, fmt.Errorf("departure_time and arrival_time cannot both be specified")
	}
	if (len(r.TransitMode) != 0 || r.TransitRoutingPreference != "") && r.Mode != maps.TravelModeTransit {
		return 2, fmt.Errorf("transit options require transit mode")
	}
	var options []maps.ClientOption
	if *apiKey != "" {
		options = append(options, maps.WithAPIKey(*apiKey), maps.WithRateLimit(2))
	} else if *clientID != "" || *signature != "" {
		options = append(options, maps.WithClientIDAndSignature(*clientID, *signature))
	} else {
		return 2, fmt.Errorf("please specify an API key, or an eligible existing customer's legacy client ID and URL signing secret")
	}
	client, err := maps.NewClient(append(options, clientOptions...)...)
	if err != nil {
		return 2, err
	}
	if *iterations == 1 {
		routes, waypoints, err := client.Directions(ctx, r)
		if err != nil {
			return 1, err
		}
		if _, err := pretty.Fprintf(stdout, "%# v\n%# v\n", waypoints, routes); err != nil {
			return 1, err
		}
		return 0, nil
	}
	done := make(chan iterationResult)
	for i := 0; i < *iterations; i++ {
		go func(i int) {
			start := time.Now()
			_, _, err := client.Directions(ctx, r)
			done <- iterationResult{
				result: fmt.Sprintf("Iteration %2d: round trip %.2f seconds", i, time.Since(start).Seconds()),
				err:    err,
			}
		}(i)
	}
	var serviceErr, outputErr error
	for i := 0; i < *iterations; i++ {
		result := <-done
		var err error
		if result.err != nil {
			if serviceErr == nil {
				serviceErr = result.err
			}
			_, err = fmt.Fprintf(stdout, "error: %+v\n", result.err)
		} else {
			_, err = fmt.Fprintln(stdout, result.result)
		}
		if err != nil && outputErr == nil {
			outputErr = err
		}
	}
	if outputErr != nil {
		return 1, outputErr
	}
	return 0, serviceErr
}

type iterationResult struct {
	result string
	err    error
}
