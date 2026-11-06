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

// Package main contains a simple command line tool for Places API Text Search
// Documentation: https://developers.google.com/places/web-service/search#TextSearchRequests
//
// Authentication requires -key. The former -client_id and -signature flags
// were removed because Places does not support Premium Plan client IDs.
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
	flags := flag.NewFlagSet("nearbysearch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API Key for using Google Maps API.")
	location := flags.String("location", "", "Search latitude,longitude.")
	radius := flags.Uint("radius", 0, "Search radius in meters.")
	keyword := flags.String("keyword", "", "Search keyword.")
	language := flags.String("language", "", "Result language.")
	minPrice := flags.String("minprice", "", "Minimum price level (0-4).")
	maxPrice := flags.String("maxprice", "", "Maximum price level (0-4), independent of -minprice.")
	name := flags.String("name", "", "Place name terms.")
	openNow := flags.Bool("open_now", false, "Only open places.")
	rankBy := flags.String("rankby", "", "prominence or distance.")
	placeType := flags.String("type", "", "Place type.")
	pageToken := flags.String("pagetoken", "", "Next page token.")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if flags.NArg() != 0 {
		return 2, fmt.Errorf("unexpected positional arguments")
	}
	if *apiKey == "" {
		return 2, fmt.Errorf("Please specify an API Key.")
	}
	r := &maps.NearbySearchRequest{
		Radius: *radius, Keyword: *keyword, Language: *language,
		Name: *name, OpenNow: *openNow, PageToken: *pageToken,
	}
	if *location != "" {
		if strings.Count(*location, ",") != 1 {
			return 2, fmt.Errorf("location must be latitude,longitude")
		}
		l, err := maps.ParseLatLng(*location)
		if err != nil {
			return 2, err
		}
		r.Location = &l
	}
	var err error
	if *minPrice != "" {
		r.MinPrice, err = parsePriceLevel(*minPrice)
		if err != nil {
			return 2, err
		}
	}
	if *maxPrice != "" {
		r.MaxPrice, err = parsePriceLevel(*maxPrice)
		if err != nil {
			return 2, err
		}
	}
	switch *rankBy {
	case "":
	case "prominence":
		r.RankBy = maps.RankByProminence
	case "distance":
		r.RankBy = maps.RankByDistance
	default:
		return 2, fmt.Errorf("unknown rank by: %q", *rankBy)
	}
	if *placeType != "" {
		r.Type, err = maps.ParsePlaceType(*placeType)
		if err != nil {
			return 2, err
		}
	}
	if r.PageToken == "" {
		if r.Location == nil {
			return 2, fmt.Errorf("location required without pagetoken")
		}
		if r.RankBy == maps.RankByDistance {
			if r.Radius != 0 {
				return 2, fmt.Errorf("radius cannot be used with rankby distance")
			}
			if r.Keyword == "" && r.Name == "" && r.Type == "" {
				return 2, fmt.Errorf("rankby distance requires keyword, name or type")
			}
		} else if r.Radius == 0 {
			return 2, fmt.Errorf("radius required without rankby distance")
		}
	}
	options := make([]maps.ClientOption, 0, len(clientOptions)+1)
	options = append(options, clientOptions...)
	options = append(options, maps.WithAPIKey(*apiKey))
	client, err := maps.NewClient(options...)
	if err != nil {
		return 2, err
	}
	resp, err := client.NearbySearch(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

func parsePriceLevel(value string) (maps.PriceLevel, error) {
	switch value {
	case "0":
		return maps.PriceLevelFree, nil
	case "1":
		return maps.PriceLevelInexpensive, nil
	case "2":
		return maps.PriceLevelModerate, nil
	case "3":
		return maps.PriceLevelExpensive, nil
	case "4":
		return maps.PriceLevelVeryExpensive, nil
	default:
		return "", fmt.Errorf("unknown price level: %q", value)
	}
}
