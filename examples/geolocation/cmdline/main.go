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

// Package main demonstrates Geolocation API input, estimated coordinates and
// accuracy in meters. Enable Geolocation API and billing before real requests.
// See https://developers.google.com/maps/documentation/geolocation.
//
// Pass one SDK-shaped JSON object with explicit considerIp:true or false through
// -request; cellTowers and wifiAccessPoints use the existing SDK models.
// Device identifiers and IP addresses are privacy-sensitive: send only data you
// are authorized to share. See examples/README.md for fictional input examples.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"googlemaps.github.io/maps"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Client options let offline tests use the real SDK against a local HTTP fixture.
func run(args []string, stdout, stderr io.Writer, clientOptions ...maps.ClientOption) int {
	flags := flag.NewFlagSet("geolocation", flag.ContinueOnError)
	flags.SetOutput(stderr)
	key := flags.String("key", "", "API key for a billing-enabled project with Geolocation API enabled (required).")
	request := flags.String("request", "", "One GeolocationRequest JSON object with explicit considerIp:true or considerIp:false (required).")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: geolocation -key API_KEY -request '{\"considerIp\":false,\"cellTowers\":[...],\"wifiAccessPoints\":[...]}'")
		fmt.Fprintln(stderr, "Enable Geolocation API and billing; requests can incur charges.")
		fmt.Fprintln(stderr, "considerIp:true permits IP fallback; false disables it. No default choice is made.")
		fmt.Fprintln(stderr, "Cell/Wi-Fi identifiers and IP addresses are privacy-sensitive; only share authorized data.")
		fmt.Fprintln(stderr, "Fictional shape example (not expected to locate):")
		fmt.Fprintln(stderr, `  -request '{"considerIp":false,"cellTowers":[{"cellId":42,"mobileCountryCode":999,"mobileNetworkCode":99,"age":0}],"wifiAccessPoints":[{"macAddress":"02:00:00:00:00:01","signalStrength":-45,"channel":6}]}'`)
		fmt.Fprintln(stderr, "Output: latitude, longitude and accuracy_m (meters). Flags may be visible in process listings.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	if strings.TrimSpace(*key) == "" {
		fmt.Fprintln(stderr, "-key is required")
		return 2
	}
	if *request == "" {
		fmt.Fprintln(stderr, "-request is required")
		return 2
	}
	r, err := parseRequest(*request)
	if err != nil {
		fmt.Fprintf(stderr, "invalid -request: %v\n", err)
		return 2
	}

	clientOptions = append(clientOptions, maps.WithAPIKey(*key))
	client, err := maps.NewClient(clientOptions...)
	if err == nil {
		var result *maps.GeolocationResult
		result, err = client.Geolocate(context.Background(), r)
		if err == nil {
			if _, err := fmt.Fprintf(stdout, "latitude: %g\nlongitude: %g\naccuracy_m: %g\n", result.Location.Lat, result.Location.Lng, result.Accuracy); err != nil {
				fmt.Fprintln(stderr, "geolocation: writing results failed")
				return 1
			}
			return 0
		}
	}
	// Transport errors may contain a request URL, including its API key.
	message := strings.ReplaceAll(err.Error(), url.QueryEscape(*key), "[redacted]")
	message = strings.ReplaceAll(message, *key, "[redacted]")
	fmt.Fprintf(stderr, "geolocation: %s\n", message)
	return 1
}

func parseRequest(input string) (*maps.GeolocationRequest, error) {
	// The SDK bool has no presence marker. Track presence locally without changing
	// the SDK model or replacing legitimate false/zero values with defaults.
	var request struct {
		maps.GeolocationRequest
		ConsiderIP *bool `json:"considerIp"`
	}
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return nil, err
	}
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one JSON object")
	}
	if request.ConsiderIP == nil {
		return nil, fmt.Errorf("considerIp must explicitly be true or false")
	}
	request.GeolocationRequest.ConsiderIP = *request.ConsiderIP
	return &request.GeolocationRequest, nil
}
