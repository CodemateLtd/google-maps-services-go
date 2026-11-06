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
	flags := flag.NewFlagSet("placedetails", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API Key for using Google Maps API.")
	placeID := flags.String("place_id", "", "Textual identifier that uniquely identifies a place.")
	fields := flags.String("fields", "", "Comma separated list of Fields")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0, nil
		}
		return 2, nil
	}
	if *apiKey == "" {
		return 2, fmt.Errorf("Please specify an API Key.")
	}
	if *placeID == "" {
		return 2, fmt.Errorf("Please specify a place ID.")
	}
	r := &maps.PlaceDetailsRequest{PlaceID: *placeID}
	if *fields != "" {
		f, err := parseFields(*fields)
		if err != nil {
			return 2, err
		}
		r.Fields = f
	}
	options := make([]maps.ClientOption, 0, len(clientOptions)+1)
	options = append(options, clientOptions...)
	options = append(options, maps.WithAPIKey(*apiKey))
	client, err := maps.NewClient(options...)
	if err != nil {
		return 1, err
	}
	resp, err := client.PlaceDetails(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

func parseFields(fields string) ([]maps.PlaceDetailsFieldMask, error) {
	var res []maps.PlaceDetailsFieldMask
	for _, s := range strings.Split(fields, ",") {
		f, err := maps.ParsePlaceDetailsFieldMask(s)
		if err != nil {
			return nil, err
		}
		res = append(res, f)
	}
	return res, nil
}
