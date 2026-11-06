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

// Package main contains a simple command line tool for Geocoding API
// Documentation: https://developers.google.com/maps/documentation/geocoding/
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
	flags := flag.NewFlagSet("geocoding", flag.ContinueOnError)
	flags.SetOutput(stderr)
	apiKey := flags.String("key", "", "API key for Google Maps API access (recommended).")
	clientID := flags.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature := flags.String("signature", "", "URL signing secret for the eligible existing customer's legacy client ID.")
	address := flags.String("address", "", "The street address to geocode.")
	components := flags.String("components", "", "Component filters as name:value pairs separated by |, with non-empty names and values.")
	bounds := flags.String("bounds", "", "Bounding box as southwest_lat,southwest_lng|northeast_lat,northeast_lng.")
	language := flags.String("language", "", "The language in which to return results.")
	region := flags.String("region", "", "The region code, specified as a ccTLD two-character value.")
	latlng := flags.String("latlng", "", "Latitude,longitude. Uses ReverseGeocode only without address or components; mixed inputs retain Geocode.")
	resultType := flags.String("result_type", "", "Address types separated by |.")
	locationType := flags.String("location_type", "", "Location types separated by |.")
	enableAddressDescriptor := flags.String("enable_address_descriptor", "", "True or False. Whether to return Address Descriptors.")
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
	r := &maps.GeocodingRequest{Address: *address, Language: *language, Region: *region}
	for _, input := range []struct {
		value string
		parse func(string, *maps.GeocodingRequest) error
	}{
		{*components, parseComponents},
		{*bounds, parseBounds},
		{*latlng, parseLatLng},
	} {
		if err := input.parse(input.value, r); err != nil {
			return 2, err
		}
	}
	parseResultType(*resultType, r)
	parseLocationType(*locationType, r)
	parseEnableAddressDescriptor(*enableAddressDescriptor, r)
	if r.Address == "" && len(r.Components) == 0 && r.LatLng == nil {
		return 2, fmt.Errorf("address, components or latlng is required")
	}
	var resp maps.GeocodingResponse
	if r.LatLng != nil && r.Address == "" && len(r.Components) == 0 {
		resp, err = client.ReverseGeocode(ctx, r)
	} else {
		resp, err = client.Geocode(ctx, r)
	}
	if err != nil {
		return 1, err
	}
	if _, err := pretty.Fprintf(stdout, "%# v\n", resp); err != nil {
		return 1, err
	}
	return 0, nil
}

func parseComponents(components string, r *maps.GeocodingRequest) error {
	if components == "" {
		return nil
	}
	if r.Components == nil {
		r.Components = make(map[maps.Component]string)
	}

	c := strings.Split(components, "|")
	for _, cf := range c {
		i := strings.Split(cf, ":")
		if len(i) != 2 || i[0] == "" || i[1] == "" {
			return fmt.Errorf("parseComponents: expected name:value with non-empty name and value, got %q", cf)
		}
		switch i[0] {
		case "route":
			r.Components[maps.ComponentRoute] = i[1]
		case "locality":
			r.Components[maps.ComponentLocality] = i[1]
		case "administrative_area":
			r.Components[maps.ComponentAdministrativeArea] = i[1]
		case "postal_code":
			r.Components[maps.ComponentPostalCode] = i[1]
		case "country":
			r.Components[maps.ComponentCountry] = i[1]
		default:
			return fmt.Errorf("parseComponents: component name %q unknown", i[0])
		}
	}
	return nil
}

func parseBounds(bounds string, r *maps.GeocodingRequest) error {
	if bounds == "" {
		return nil
	}
	b := strings.Split(bounds, "|")
	if len(b) != 2 {
		return fmt.Errorf("parseBounds: expected southwest_lat,southwest_lng|northeast_lat,northeast_lng, got %q", bounds)
	}
	if strings.Count(b[0], ",") != 1 || strings.Count(b[1], ",") != 1 {
		return fmt.Errorf("parseBounds: each corner must contain exactly latitude,longitude, got %q", bounds)
	}
	sw, err := maps.ParseLatLng(b[0])
	if err != nil {
		return fmt.Errorf("parseBounds: southwest corner: %w", err)
	}
	ne, err := maps.ParseLatLng(b[1])
	if err != nil {
		return fmt.Errorf("parseBounds: northeast corner: %w", err)
	}
	r.Bounds = &maps.LatLngBounds{
		NorthEast: ne,
		SouthWest: sw,
	}
	return nil
}

func parseLatLng(latlng string, r *maps.GeocodingRequest) error {
	if latlng == "" {
		return nil
	}
	if strings.Count(latlng, ",") != 1 {
		return fmt.Errorf("parseLatLng: expected exactly latitude,longitude, got %q", latlng)
	}
	location, err := maps.ParseLatLng(latlng)
	if err != nil {
		return fmt.Errorf("parseLatLng: %w", err)
	}
	r.LatLng = &location
	return nil
}

func parseResultType(resultType string, r *maps.GeocodingRequest) {
	if resultType != "" {
		r.ResultType = strings.Split(resultType, "|")
	}
}

func parseLocationType(locationType string, r *maps.GeocodingRequest) {
	if locationType != "" {
		for _, l := range strings.Split(locationType, "|") {
			switch l {
			case "ROOFTOP":
				r.LocationType = append(r.LocationType, maps.GeocodeAccuracyRooftop)
			case "RANGE_INTERPOLATED":
				r.LocationType = append(r.LocationType, maps.GeocodeAccuracyRangeInterpolated)
			case "GEOMETRIC_CENTER":
				r.LocationType = append(r.LocationType, maps.GeocodeAccuracyGeometricCenter)
			case "APPROXIMATE":
				r.LocationType = append(r.LocationType, maps.GeocodeAccuracyApproximate)
			}
		}

	}
}

func parseEnableAddressDescriptor(enableAddressDescriptor string, r *maps.GeocodingRequest) {
	if enableAddressDescriptor == "True" {
		r.EnableAddressDescriptor = true
	} else {
		r.EnableAddressDescriptor = false
	}
}
