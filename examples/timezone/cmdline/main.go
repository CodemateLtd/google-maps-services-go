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

// Package main contains a simple command line tool for Timezone API
// Directions docs: https://developers.google.com/maps/documentation/timezone/
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

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
	flags := flag.NewFlagSet("timezone", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API key for Google Maps API access (recommended).")
	clientID := flags.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature := flags.String("signature", "", "URL signing secret for the eligible existing customer's legacy client ID.")
	location := flags.String("location", "", "Location as latitude,longitude.")
	timestamp := flags.String("timestamp", "", "Desired time as seconds since midnight, January 1, 1970 UTC.")
	language := flags.String("language", "", "The language in which to return results.")
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
	seconds, err := strconv.ParseInt(*timestamp, 10, 64)
	if err != nil {
		return 2, fmt.Errorf("timestamp: %w", err)
	}
	r := &maps.TimezoneRequest{Language: *language, Timestamp: time.Unix(seconds, 0)}
	if err := parseLocation(*location, r); err != nil {
		return 2, err
	}
	resp, err := client.Timezone(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

func parseLocation(location string, r *maps.TimezoneRequest) error {
	if strings.Count(location, ",") != 1 {
		return fmt.Errorf("location: expected latitude,longitude, got %q", location)
	}
	point, err := maps.ParseLatLng(location)
	if err != nil {
		return fmt.Errorf("location: %w", err)
	}
	r.Location = &point
	return nil
}
