// Copyright 2017 Google Inc. All Rights Reserved.
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

// Package main contains a simple command line tool for Static Maps API
// Documentation: https://developers.google.com/maps/documentation/static-maps/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/kr/pretty"
	"googlemaps.github.io/maps"
)

var (
	apiKey    = flag.String("key", "", "API key for Google Maps API access (recommended); may be combined with -signature for URL signing.")
	clientID  = flag.String("client_id", "", "Legacy Premium Plan client ID for eligible existing customers; use -key otherwise.")
	signature = flag.String("signature", "", "URL signing secret, not a precomputed signature; signs an API key or an eligible existing customer's legacy client ID.")
	center    = flag.String("center", "", "Center the center of the map, equidistant from all edges of the map.")
	zoom      = flag.Int("zoom", -1, "Zoom the zoom level of the map, which determines the magnification level of the map.")
	size      = flag.String("size", "", "Size defines the rectangular dimensions of the map image.")
	scale     = flag.Int("scale", -1, "Scale affects the number of pixels that are returned.")
	format    = flag.String("format", "", "Format defines the format of the resulting image.")
	maptype   = flag.String("maptype", "", "Maptype defines the type of map to construct.")
	mapid     = flag.String("mapid", "", "MapId defines the mapid to use.")
	language  = flag.String("language", "", "Language defines the language to use for display of labels on map tiles.")
	region    = flag.String("region", "", "Region the appropriate borders to display, based on geo-political sensitivities.")
	markers   markerFlags
	paths     pathFlags
	visible   visibleFlags
	styles    styleFlags
)

func init() {
	flag.Var(&markers, "marker", `Repeatable JSON marker: {"color":"blue","label":"A","size":"mid","locations":[[lat,lng]],"icon":{"url":"https://example.com/pin.png","anchor":"bottom","scale":2}}. Omit icon for ordinary markers; omit color/label/size with icon.`)
	flag.Var(&paths, "path", `Repeatable JSON path: {"color":"0xff0000ff","fill_color":"0x0000ff40","weight":3,"geodesic":true,"locations":[[lat,lng],[lat,lng]]}. Requires at least two points; use explicit -center/-zoom for paths without markers.`)
	flag.Var(&visible, "visible", "Repeatable latitude,longitude point to keep visible. Use explicit -center/-zoom without markers.")
	flag.Var(&styles, "style", "Repeatable Static Maps style string, e.g. feature:water|element:geometry|color:0x0000ff.")
}

// point keeps the CLI's coordinate pairs distinct from the SDK's LatLng objects.
type point maps.LatLng

func (p *point) UnmarshalJSON(data []byte) error {
	var pair []*float64
	if err := json.Unmarshal(data, &pair); err != nil {
		return fmt.Errorf("coordinate must be [latitude,longitude]: %v", err)
	}
	if len(pair) != 2 {
		return fmt.Errorf("coordinate must contain exactly latitude and longitude")
	}
	if pair[0] == nil || pair[1] == nil {
		return fmt.Errorf("coordinate latitude and longitude must be numbers, not null")
	}
	location, err := checkedPoint(*pair[0], *pair[1])
	if err != nil {
		return err
	}
	*p = point(location)
	return nil
}

func checkedPoint(lat, lng float64) (maps.LatLng, error) {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 ||
		math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return maps.LatLng{}, fmt.Errorf("coordinates must be finite with latitude in [-90,90] and longitude in [-180,180]")
	}
	return maps.LatLng{Lat: lat, Lng: lng}, nil
}

func decodeOverlay(value string, target interface{}) error {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid overlay JSON: %v", err)
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}

func locations(points []point) []maps.LatLng {
	result := make([]maps.LatLng, len(points))
	for i, p := range points {
		result[i] = maps.LatLng(p)
	}
	return result
}

type markerFlags []maps.Marker

func (m *markerFlags) String() string { return "" }

func (m *markerFlags) Set(value string) error {
	var input struct {
		Color     string  `json:"color"`
		Label     string  `json:"label"`
		Size      string  `json:"size"`
		Locations []point `json:"locations"`
		Icon      *struct {
			URL    string `json:"url"`
			Anchor string `json:"anchor"`
			Scale  int    `json:"scale"`
		} `json:"icon"`
	}
	if err := decodeOverlay(value, &input); err != nil {
		return err
	}
	if len(input.Locations) == 0 {
		return fmt.Errorf("marker requires at least one location")
	}
	marker := maps.Marker{Color: input.Color, Label: input.Label, Size: input.Size}
	if input.Icon != nil {
		icon := input.Icon
		u, err := url.Parse(icon.URL)
		if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(icon.URL, "| \t\r\n") {
			return fmt.Errorf("icon URL must be an absolute HTTP or HTTPS URL without credentials, whitespace or pipes")
		}
		if !validAnchor(icon.Anchor) {
			return fmt.Errorf("icon anchor must be a named position or non-negative integer x,y")
		}
		if icon.Scale != 0 && icon.Scale != 1 && icon.Scale != 2 && icon.Scale != 4 {
			return fmt.Errorf("icon scale must be 1, 2 or 4 (or omitted)")
		}
		if input.Color != "" || input.Label != "" || input.Size != "" {
			return fmt.Errorf("icon cannot be combined with marker color, label or size")
		}
		marker.CustomIcon = maps.CustomIcon{IconURL: icon.URL, Anchor: maps.Anchor(icon.Anchor), Scale: icon.Scale}
	}
	marker.Location = locations(input.Locations)
	*m = append(*m, marker)
	return nil
}

func validAnchor(anchor string) bool {
	switch anchor {
	case "", "top", "bottom", "left", "right", "center", "topleft", "topright", "bottomleft", "bottomright":
		return true
	}
	pair := strings.Split(anchor, ",")
	if len(pair) != 2 {
		return false
	}
	for _, value := range pair {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return false
		}
	}
	return true
}

type pathFlags []maps.Path

func (p *pathFlags) String() string { return "" }

func (p *pathFlags) Set(value string) error {
	var input struct {
		Color     string  `json:"color"`
		FillColor string  `json:"fill_color"`
		Weight    int     `json:"weight"`
		Geodesic  bool    `json:"geodesic"`
		Locations []point `json:"locations"`
	}
	if err := decodeOverlay(value, &input); err != nil {
		return err
	}
	if len(input.Locations) < 2 {
		return fmt.Errorf("path requires at least two locations")
	}
	*p = append(*p, maps.Path{Color: input.Color, FillColor: input.FillColor, Weight: input.Weight, Geodesic: input.Geodesic, Location: locations(input.Locations)})
	return nil
}

type visibleFlags []maps.LatLng

func (v *visibleFlags) String() string { return "" }

func (v *visibleFlags) Set(value string) error {
	pair := strings.Split(value, ",")
	if len(pair) != 2 {
		return fmt.Errorf("visible point must be latitude,longitude")
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(pair[0]), 64)
	lng, lngErr := strconv.ParseFloat(strings.TrimSpace(pair[1]), 64)
	if latErr != nil || lngErr != nil {
		return fmt.Errorf("visible point must contain numeric latitude and longitude")
	}
	location, err := checkedPoint(lat, lng)
	if err != nil {
		return err
	}
	*v = append(*v, location)
	return nil
}

type styleFlags []string

func (s *styleFlags) String() string { return "" }

func (s *styleFlags) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("style must not be empty")
	}
	*s = append(*s, value)
	return nil
}

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
		if *signature != "" {
			client, err = maps.NewClient(maps.WithAPIKeyAndSignature(*apiKey, *signature))
		} else {
			client, err = maps.NewClient(maps.WithAPIKey(*apiKey))
		}
	} else if *clientID != "" || *signature != "" {
		client, err = maps.NewClient(maps.WithClientIDAndSignature(*clientID, *signature))
	} else {
		usageAndExit("Please specify an API key (recommended; optionally with a URL signing secret), or an eligible existing customer's legacy client ID and URL signing secret.")
	}
	check(err)

	r := &maps.StaticMapRequest{
		Center:    *center,
		Zoom:      *zoom,
		Size:      *size,
		Scale:     *scale,
		Format:    maps.Format(*format),
		Language:  *language,
		Region:    *region,
		MapType:   maps.MapType(*maptype),
		MapId:     *mapid,
		Markers:   markers,
		Paths:     paths,
		Visible:   visible,
		MapStyles: styles,
	}

	resp, err := client.StaticMap(context.Background(), r)
	check(err)

	pretty.Println(resp)
}
