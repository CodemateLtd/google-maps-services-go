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
	"log"
	"os"
	"strings"

	"github.com/kr/pretty"
	"googlemaps.github.io/maps"
)

var (
	apiKey       = flag.String("key", "", "API Key for using Google Maps API.")
	clientID     = flag.String("client_id", "", "ClientID for Maps for Work API access.")
	signature    = flag.String("signature", "", "Signature for Maps for Work API access.")
	address      = flag.String("address", "", "The street address that you want to geocode, in the format used by the national postal service of the country concerned.")
	components   = flag.String("components", "", "Component filters as name:value pairs separated by |, with non-empty names and values.")
	bounds       = flag.String("bounds", "", "The bounding box used to bias results, as southwest_lat,southwest_lng|northeast_lat,northeast_lng.")
	language     = flag.String("language", "", "The language in which to return results.")
	region       = flag.String("region", "", "The region code, specified as a ccTLD two-character value.")
	latlng       = flag.String("latlng", "", "The textual latitude,longitude pair for which you wish to obtain the closest, human-readable address.")
	resultType   = flag.String("result_type", "", "One or more address types, separated by a pipe (|).")
	locationType = flag.String("location_type", "", "One or more location types, separated by a pipe (|).")
	enableAddressDescriptor = flag.String("enable_address_descriptor", "", "True or False.  Whether to return the Address Descriptors in the response.")
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
	if *apiKey != "" {
		client, err = maps.NewClient(maps.WithAPIKey(*apiKey))
	} else if *clientID != "" || *signature != "" {
		client, err = maps.NewClient(maps.WithClientIDAndSignature(*clientID, *signature))
	} else {
		usageAndExit("Please specify an API Key, or Client ID and Signature.")
	}
	check(err)

	r := &maps.GeocodingRequest{
		Address:  *address,
		Language: *language,
		Region:   *region,
	}

	check(parseComponents(*components, r))
	check(parseBounds(*bounds, r))
	check(parseLatLng(*latlng, r))
	parseResultType(*resultType, r)
	parseLocationType(*locationType, r)
	parseEnableAddressDescriptor(*enableAddressDescriptor, r)

	resp, err := client.Geocode(context.Background(), r)
	check(err)

	pretty.Println(resp)
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
