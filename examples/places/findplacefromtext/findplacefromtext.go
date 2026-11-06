// Copyright 2018 Google Inc. All Rights Reserved.
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

// Package main contains a simple command line tool for Find Place From Text API
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
	flags := flag.NewFlagSet("findplacefromtext", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API Key for using Google Maps API.")
	input := flags.String("input", "", "Place name, address or phone number.")
	inputType := flags.String("inputtype", "", "textquery or phonenumber.")
	fields := flags.String("fields", "", "Comma separated field masks.")
	locationBias := flags.String("locationbias", "", "ipbias, point, circle or rectangle.")
	point := flags.String("point", "", "Point latitude,longitude.")
	center := flags.String("center", "", "Circle center latitude,longitude.")
	radius := flags.Int("radius", 0, "Circle radius.")
	southwest := flags.String("southwest", "", "Rectangle southwest latitude,longitude.")
	northeast := flags.String("northeast", "", "Rectangle northeast latitude,longitude.")
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
	if *input == "" {
		return 2, fmt.Errorf("input required")
	}
	r := &maps.FindPlaceFromTextRequest{Input: *input}
	switch *inputType {
	case "textquery":
		r.InputType = maps.FindPlaceFromTextInputTypeTextQuery
	case "phonenumber":
		r.InputType = maps.FindPlaceFromTextInputTypePhoneNumber
	default:
		return 2, fmt.Errorf("unknown input type: %q", *inputType)
	}
	if *locationBias != "" {
		lb, err := maps.ParseFindPlaceFromTextLocationBiasType(*locationBias)
		if err != nil {
			return 2, err
		}
		r.LocationBias = lb
		switch lb {
		case maps.FindPlaceFromTextLocationBiasPoint:
			l, err := parseLocation(*point)
			if err != nil {
				return 2, err
			}
			r.LocationBiasPoint = &l
		case maps.FindPlaceFromTextLocationBiasCircular:
			l, err := parseLocation(*center)
			if err != nil {
				return 2, err
			}
			r.LocationBiasCenter = &l
			r.LocationBiasRadius = *radius
			if *radius == 0 {
				return 2, fmt.Errorf("circle radius required")
			}
		case maps.FindPlaceFromTextLocationBiasRectangular:
			sw, err := parseLocation(*southwest)
			if err != nil {
				return 2, err
			}
			ne, err := parseLocation(*northeast)
			if err != nil {
				return 2, err
			}
			r.LocationBiasSouthWest, r.LocationBiasNorthEast = &sw, &ne
		}
	}
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
		return 2, err
	}
	resp, err := client.FindPlaceFromText(ctx, r)
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

func parseFields(fields string) ([]maps.PlaceSearchFieldMask, error) {
	var res []maps.PlaceSearchFieldMask
	for _, s := range strings.Split(fields, ",") {
		f, err := maps.ParsePlaceSearchFieldMask(s)
		if err != nil {
			return nil, err
		}
		res = append(res, f)
	}
	return res, nil
}

func parseLocation(value string) (maps.LatLng, error) {
	if strings.Count(value, ",") != 1 {
		return maps.LatLng{}, fmt.Errorf("location must be latitude,longitude")
	}
	return maps.ParseLatLng(value)
}
