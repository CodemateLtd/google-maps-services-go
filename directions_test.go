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

package maps

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/stretchr/testify/require"
)

const apiKey = "AIzaNotReallyAnAPIKey"

type countingServer struct {
	s          *httptest.Server
	successful int
	failed     []string
}

// mockServerForQuery returns a mock server that only responds to a particular query string.
func mockServerForQuery(query string, code int, body string) *countingServer {
	server := &countingServer{}

	server.s = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if query != "" && r.URL.RawQuery != query {
			dmp := diffmatchpatch.New()
			diffs := dmp.DiffMain(query, r.URL.RawQuery, false)
			log.Printf("Query != Expected Query: %s", dmp.DiffPrettyText(diffs))
			server.failed = append(server.failed, r.URL.RawQuery)
			http.Error(w, "fail", 999)
			return
		}
		server.successful++

		w.WriteHeader(code)
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		fmt.Fprintln(w, body)
	}))

	return server
}

// Create a mock HTTP Server that will return a response with HTTP code and body.
func mockServer(code int, body string) *httptest.Server {
	serv := mockServerForQuery("", code, body)
	return serv.s
}

func TestDirectionsTransit(t *testing.T) {
	// Route from Google Sydney to Glebe Pt Rd. Steps and some polylines have
	// been removed.
	response := `{
   "geocoded_waypoints" : [
      {
         "geocoder_status" : "OK",
         "partial_match" : true,
         "place_id" : "ChIJ8UadyjeuEmsRDt5QbiDg720",
         "types" : [ "premise" ]
      },
      {
         "geocoder_status" : "OK",
         "place_id" : "ChIJl7hmVNOvEmsRcW-sYgALB78",
         "types" : [ "route" ]
      }
   ],
   "routes" : [
      {
         "bounds" : {
            "northeast" : {
               "lat" : -33.8668939,
               "lng" : 151.1952284
            },
            "southwest" : {
               "lat" : -33.8785317,
               "lng" : 151.1856793
            }
         },
         "copyrights" : "Map data ©2016 Google",
         "legs" : [
            {
               "arrival_time" : {
                  "text" : "4:09pm",
                  "time_zone" : "Australia/Sydney",
                  "value" : 1455512950
               },
               "departure_time" : {
                  "text" : "4:00pm",
                  "time_zone" : "Australia/Sydney",
                  "value" : 1455512400
               },
               "distance" : {
                  "text" : "2.2 km",
                  "value" : 2241
               },
               "duration" : {
                  "text" : "9 mins",
                  "value" : 550
               },
               "end_address" : "Glebe Point Rd, Glebe NSW 2037, Australia",
               "end_location" : {
                  "lat" : -33.8785317,
                  "lng" : 151.1859855
               },
               "start_address" : "Workplace 6, 48 Pirrama Rd, Pyrmont NSW 2009, Australia",
               "start_location" : {
                  "lat" : -33.8675125,
                  "lng" : 151.1950229
               },
               "steps" : [
               ],
               "via_waypoint" : []
            }
         ],
         "overview_polyline" : {
            "points" : ""
         },
         "summary" : "",
         "warnings" : [
            "Walking directions are in beta.    Use caution – This route may be missing sidewalks or pedestrian paths."
         ],
         "waypoint_order" : []
      }
   ],
   "status" : "OK"
}`

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "Google Sydney",
		Destination: "Glebe Pt Rd, Glebe",
		Mode:        TravelModeTransit,
	}

	resp, err := c.Directions(context.Background(), r)

	if len(resp.Routes) != 1 {
		t.Fatalf("Expected length of response is 1, was %+v", len(resp.Routes))
	}
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	tzSydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Errorf("coudln't load expected timezone Australia/Sydney: %v", err)
	}
	arrivalTime := time.Date(2016, 2, 15, 16, 9, 10, 0, tzSydney)
	departureTime := time.Date(2016, 2, 15, 16, 0, 0, 0, tzSydney)

	var legs []*Leg
	legs = append(legs, &Leg{
		Steps:         make([]*Step, 0),
		Distance:      Distance{HumanReadable: "2.2 km", Meters: 2241},
		Duration:      time.Duration(550) * time.Second,
		ArrivalTime:   arrivalTime,
		DepartureTime: departureTime,
		StartLocation: LatLng{Lat: -33.8675125, Lng: 151.1950229},
		EndLocation:   LatLng{Lat: -33.8785317, Lng: 151.1859855},
		StartAddress:  "Workplace 6, 48 Pirrama Rd, Pyrmont NSW 2009, Australia",
		EndAddress:    "Glebe Point Rd, Glebe NSW 2037, Australia",
		ViaWaypoint:   make([]*ViaWaypoint, 0),
	})

	correctResponse := &Route{
		OverviewPolyline: Polyline{},
		Legs:             legs,
		Bounds: LatLngBounds{
			NorthEast: LatLng{Lat: -33.8668939, Lng: 151.1952284},
			SouthWest: LatLng{Lat: -33.8785317, Lng: 151.1856793},
		},
		Copyrights: "Map data ©2016 Google",
		Warnings: []string{
			"Walking directions are in beta.    Use caution – This route may be missing sidewalks or pedestrian paths.",
		},
		WaypointOrder: make([]int, 0),
	}

	if actualResponse := &resp.Routes[0]; !reflect.DeepEqual(actualResponse, correctResponse) {
		t.Errorf("expected %+v, was %+v", correctResponse, actualResponse)
	}
}

func TestDirectionsSydneyToParramatta(t *testing.T) {

	// Route from Sydney to Parramatta with most steps elided.
	response := `{
   "routes" : [
      {
         "bounds" : {
            "northeast" : {
               "lat" : -33.8150985,
               "lng" : 151.2070825
            },
            "southwest" : {
               "lat" : -33.8770049,
               "lng" : 151.0031658
            }
         },
         "copyrights" : "Map data ©2015 Google",
         "legs" : [
            {
               "distance" : {
                  "text" : "23.8 km",
                  "value" : 23846
               },
               "duration" : {
                  "text" : "37 mins",
                  "value" : 2214
               },
               "end_address" : "Parramatta NSW, Australia",
               "end_location" : {
                  "lat" : -33.8150985,
                  "lng" : 151.0031658
               },
               "start_address" : "Sydney NSW, Australia",
               "start_location" : {
                  "lat" : -33.8674944,
                  "lng" : 151.2070825
               },
               "steps" : [
                  {
                     "distance" : {
                        "text" : "0.4 km",
                        "value" : 366
                     },
                     "duration" : {
                        "text" : "2 mins",
                        "value" : 103
                     },
                     "end_location" : {
                        "lat" : -33.8707786,
                        "lng" : 151.206934
                     },
                     "html_instructions" : "Head \u003cb\u003esouth\u003c/b\u003e on \u003cb\u003eGeorge St\u003c/b\u003e toward \u003cb\u003eBarrack St\u003c/b\u003e",
                     "polyline" : {
                        "points" : "xvumEgs{y[V@|AH|@DdABbC@@?^@N?zD@\\?F@"
                     },
                     "start_location" : {
                        "lat" : -33.8674944,
                        "lng" : 151.2070825
                     },
                     "transit_details" : {
                        "trip_short_name": "7108"
                     },
                     "travel_mode" : "DRIVING"
                  }
               ],
               "via_waypoint" : []
            }
         ],
         "summary" : "A4 and M4"
      }
   ],
   "status" : "OK"
}`

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "Sydney",
		Destination: "Parramatta",
	}

	resp, err := c.Directions(context.Background(), r)

	if len(resp.Routes) != 1 {
		t.Fatalf("Expected length of response is 1, was %+v", len(resp.Routes))
	}
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	var steps []*Step
	steps = append(steps, &Step{
		HTMLInstructions: "Head <b>south</b> on <b>George St</b> toward <b>Barrack St</b>",
		Distance:         Distance{HumanReadable: "0.4 km", Meters: 366},
		Duration:         103000000000,
		StartLocation:    LatLng{Lat: -33.8674944, Lng: 151.2070825},
		EndLocation:      LatLng{Lat: -33.8707786, Lng: 151.206934},
		Polyline:         Polyline{Points: "xvumEgs{y[V@|AH|@DdABbC@@?^@N?zD@\\?F@"},
		Steps:            nil,
		TransitDetails: &TransitDetails{
			TripShortName: "7108",
		},
		TravelMode: "DRIVING",
	})

	var legs []*Leg
	legs = append(legs, &Leg{
		Steps:         steps,
		Distance:      Distance{HumanReadable: "23.8 km", Meters: 23846},
		Duration:      2214000000000,
		StartLocation: LatLng{Lat: -33.8674944, Lng: 151.2070825},
		EndLocation:   LatLng{Lat: -33.8150985, Lng: 151.0031658},
		StartAddress:  "Sydney NSW, Australia",
		EndAddress:    "Parramatta NSW, Australia",
		ViaWaypoint:   make([]*ViaWaypoint, 0),
	})

	correctResponse := &Route{
		Summary:          "A4 and M4",
		Legs:             legs,
		OverviewPolyline: Polyline{},
		Bounds: LatLngBounds{
			NorthEast: LatLng{Lat: -33.8150985, Lng: 151.2070825},
			SouthWest: LatLng{Lat: -33.8770049, Lng: 151.0031658},
		},
		Copyrights: "Map data ©2015 Google",
	}

	if !reflect.DeepEqual(&resp.Routes[0], correctResponse) {
		t.Errorf("expected %+v, was %+v", correctResponse, &resp.Routes[0])
	}
}

func TestDirectionsMissingOrigin(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Destination: "Parramatta",
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Missing Origin should return error")
	}
}

func TestDirectionsMissingDestination(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Origin: "Sydney",
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Missing Destination should return error")
	}
}

func TestDirectionsBadMode(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Origin:      "Sydney",
		Destination: "Parramatta",
		Mode:        "Not a Mode",
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Bad Mode should return error")
	}
}

func TestDirectionsDeclaringBothDepartureAndArrivalTime(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Origin:        "Sydney",
		Destination:   "Parramatta",
		DepartureTime: "Now",
		ArrivalTime:   "4pm",
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Declaring both DepartureTime and ArrivalTime should return error")
	}
}

func TestDirectionsTravelModeTransit(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	var transitModes []TransitMode
	transitModes = append(transitModes, TransitModeBus)
	r := &DirectionsRequest{
		Origin:      "Sydney",
		Destination: "Parramatta",
		TransitMode: transitModes,
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Declaring TransitMode without Mode=Transit should return error")
	}
}

func TestDirectionsTransitRoutingPreference(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Origin:                   "Sydney",
		Destination:              "Parramatta",
		TransitRoutingPreference: TransitRoutingPreferenceFewerTransfers,
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Declaring TransitRoutingPreference without Mode=TravelModeTransit should return error")
	}
}

func TestDirectionsWithCancelledContext(t *testing.T) {
	c, _ := NewClient(WithAPIKey(apiKey))
	r := &DirectionsRequest{
		Origin:      "Sydney",
		Destination: "Parramatta",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Directions(ctx, r); err == nil {
		t.Errorf("Cancelled context should return non-nil err")
	}
}

func TestDirectionsFailingServer(t *testing.T) {
	server := mockServer(500, `{"status" : "ERROR"}`)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "Sydney",
		Destination: "Parramatta",
	}

	if _, err := c.Directions(context.Background(), r); err == nil {
		t.Errorf("Failing server should return error")
	}
}

func TestDirectionsRequestURL(t *testing.T) {
	expectedQuery := "alternatives=true&avoid=tolls%7Cferries&destination=Parramatta&key=AIzaNotReallyAnAPIKey&language=es&mode=transit&origin=Sydney&region=es&transit_mode=rail&transit_routing_preference=fewer_transfers&units=imperial&waypoints=optimize%3Atrue%7CCharlestown%2CMA%7Cvia%3ALexington"

	server := mockServerForQuery(expectedQuery, 200, `{"status":"OK"}"`)
	defer server.s.Close()

	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))

	r := &DirectionsRequest{
		Origin:                   "Sydney",
		Destination:              "Parramatta",
		Mode:                     TravelModeTransit,
		TransitMode:              []TransitMode{TransitModeRail},
		Waypoints:                []string{"Charlestown,MA", "via:Lexington"},
		Alternatives:             true,
		Optimize:                 true,
		Avoid:                    []Avoid{AvoidTolls, AvoidFerries},
		Language:                 "es",
		Region:                   "es",
		Units:                    UnitsImperial,
		TransitRoutingPreference: TransitRoutingPreferenceFewerTransfers,
	}

	if _, err := c.Directions(context.Background(), r); err != nil {
		t.Errorf("Unexpected error in constructing request URL: %+v", err)
	}
	if server.successful != 1 {
		t.Errorf("Got URL(s) %v, want %s", server.failed, expectedQuery)
	}
}

func TestDirectionsZeroResults(t *testing.T) {
	server := mockServer(200, `{
		"routes": [],
		"geocoded_waypoints": [
			{"geocoder_status": "OK", "place_id": "origin-place", "types": ["locality"]},
			{"geocoder_status": "ZERO_RESULTS"}
		],
		"available_travel_modes": ["DRIVING", "WALKING", "FUTURE_MODE"],
		"status": "ZERO_RESULTS"
	}`)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "Boston",
		Destination: "Paris",
		Mode:        TravelModeBicycling,
	}

	result, err := c.Directions(context.Background(), r)

	if err != nil {
		t.Fatalf("Unexpected error for ZERO_RESULTS status: %v", err)
	}
	if result.Routes == nil || len(result.Routes) != 0 {
		t.Errorf("Expected non-nil empty routes, got %+v", result.Routes)
	}
	wantModes := []string{"DRIVING", "WALKING", "FUTURE_MODE"}
	if !reflect.DeepEqual(result.AvailableTravelModes, wantModes) {
		t.Errorf("Expected available modes %v, got %v", wantModes, result.AvailableTravelModes)
	}
	wantWaypoints := []GeocodedWaypoint{
		{GeocoderStatus: "OK", PlaceID: "origin-place", Types: []string{"locality"}},
		{GeocoderStatus: "ZERO_RESULTS"},
	}
	if !reflect.DeepEqual(result.GeocodedWaypoints, wantWaypoints) {
		t.Errorf("Expected geocoded waypoints %+v, got %+v", wantWaypoints, result.GeocodedWaypoints)
	}
}

func TestDirectionsResponseFields(t *testing.T) {
	server := mockServer(200, `{
		"status": "OK",
		"routes": [{
			"legs": [{
				"duration": {"value": 75, "text": "1 min"},
				"steps": [{
					"duration": {"value": 60, "text": "1 min"},
					"maneuver": "turn-left",
					"steps": [{
						"duration": {"value": 15, "text": "15 seconds"},
						"maneuver": "future-maneuver"
					}],
					"transit_details": {
						"line": {
							"vehicle": {
								"name": "Bus",
								"type": "BUS",
								"local_icon": "https://example.com/local-bus.png"
							}
						}
					}
				}]
			}]
		}]
	}`)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	result, err := c.Directions(context.Background(), &DirectionsRequest{
		Origin:      "origin",
		Destination: "destination",
		Mode:        TravelModeTransit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AvailableTravelModes != nil || result.GeocodedWaypoints != nil {
		t.Errorf("Expected omitted metadata to be nil, got %+v", result)
	}
	if len(result.Routes) != 1 || len(result.Routes[0].Legs) != 1 {
		t.Fatalf("Expected one route and leg, got %+v", result.Routes)
	}
	leg := result.Routes[0].Legs[0]
	if leg.Duration != 75*time.Second || len(leg.Steps) != 1 {
		t.Fatalf("Unexpected leg duration or steps: %+v", leg)
	}
	step := leg.Steps[0]
	if step.Duration != time.Minute || step.Maneuver != "turn-left" {
		t.Errorf("Unexpected step duration or maneuver: %+v", step)
	}
	if len(step.Steps) != 1 {
		t.Fatalf("Expected one nested step, got %+v", step.Steps)
	}
	if nested := step.Steps[0]; nested.Duration != 15*time.Second || nested.Maneuver != "future-maneuver" {
		t.Errorf("Unexpected nested step duration or maneuver: %+v", nested)
	}
	if step.TransitDetails == nil {
		t.Fatal("Expected transit details")
	}
	vehicle := step.TransitDetails.Line.Vehicle
	if vehicle.LocalIcon == nil || vehicle.LocalIcon.String() != "https://example.com/local-bus.png" {
		t.Errorf("Unexpected local signage icon: %+v", vehicle.LocalIcon)
	}
}

func TestDirectionsAPIStatusError(t *testing.T) {
	server := mockServer(200, `{
		"status": "REQUEST_DENIED",
		"error_message": "request denied",
		"routes": [],
		"available_travel_modes": ["DRIVING"]
	}`)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	result, err := c.Directions(context.Background(), &DirectionsRequest{
		Origin:      "origin",
		Destination: "destination",
	})
	if err == nil {
		t.Fatal("Expected API status error")
	}
	if !reflect.DeepEqual(result, DirectionsResult{}) {
		t.Errorf("Expected zero result on API status error, got %+v", result)
	}
}

func TestTrafficModel(t *testing.T) {
	expectedQuery := "departure_time=now&destination=Parramatta+Town+Hall&key=AIzaNotReallyAnAPIKey&mode=driving&origin=Sydney+Town+Hall&traffic_model=pessimistic"
	server := mockServerForQuery(expectedQuery, 200, `{"status":"OK"}"`)
	defer server.s.Close()

	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.s.URL))

	r := &DirectionsRequest{
		Origin:        "Sydney Town Hall",
		Destination:   "Parramatta Town Hall",
		Mode:          TravelModeDriving,
		DepartureTime: "now",
		TrafficModel:  TrafficModelPessimistic,
	}

	if _, err := c.Directions(context.Background(), r); err != nil {
		t.Errorf("Unexpected error in constructing request URL: %+v", err)
	}
	if server.successful != 1 {
		t.Errorf("Got URL(s) %v, want %s", server.failed, expectedQuery)
	}
}

func TestFare(t *testing.T) {
	// Directions response, sans steps.
	response := `{
   "geocoded_waypoints" : [
      {
         "geocoder_status" : "OK",
         "place_id" : "ChIJt4ABNBBfrIkRXFPZnsnIicw",
         "types" : [ "street_address" ]
      },
      {
         "geocoder_status" : "OK",
         "place_id" : "EjExMDAxLTExOTkgSGlsbHNib3JvdWdoIFN0LCBSYWxlaWdoLCBOQyAyNzYwMywgVVNB",
         "types" : [ "street_address" ]
      }
   ],
   "routes" : [
      {
         "bounds" : {
            "northeast" : {
               "lat" : 35.782453,
               "lng" : -78.625349
            },
            "southwest" : {
               "lat" : 35.7766535,
               "lng" : -78.6548689
            }
         },
         "copyrights" : "Map data ©2016 Google",
         "fare" : {
            "currency" : "USD",
            "text" : "$1.25",
            "value" : 1.25
         },
         "legs" : [
            {
               "arrival_time" : {
                  "text" : "8:23am",
                  "time_zone" : "America/New_York",
                  "value" : 1476620634
               },
               "departure_time" : {
                  "text" : "8:01am",
                  "time_zone" : "America/New_York",
                  "value" : 1476619311
               },
               "distance" : {
                  "text" : "2.1 mi",
                  "value" : 3408
               },
               "duration" : {
                  "text" : "22 mins",
                  "value" : 1323
               },
               "end_address" : "1001-1199 Hillsborough St, Raleigh, NC 27603, USA",
               "end_location" : {
                  "lat" : 35.782453,
                  "lng" : -78.6548689
               },
               "start_address" : "822 E Hargett St, Raleigh, NC 27601, USA",
               "start_location" : {
                  "lat" : 35.7777912,
                  "lng" : -78.625349
               },
               "traffic_speed_entry" : [],
               "via_waypoint" : []
            }
         ],
         "summary" : "",
         "warnings" : [
            "Walking directions are in beta.    Use caution – This route may be missing sidewalks or pedestrian paths."
         ],
         "waypoint_order" : []
      }
   ],
   "status" : "OK"
}`

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "35.7777111, -78.625354",
		Destination: "35.7826242, -78.6547025",
		Mode:        TravelModeTransit,
	}

	resp, err := c.Directions(context.Background(), r)

	if len(resp.Routes) != 1 {
		t.Fatalf("Expected length of response is 1, was %+v", len(resp.Routes))
	}
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	fare := &Fare{
		Currency: "USD",
		Text:     "$1.25",
		Value:    1.25,
	}

	if !reflect.DeepEqual(resp.Routes[0].Fare, fare) {
		t.Errorf("expected %+v, was %+v", fare, resp.Routes[0].Fare)
	}
}

func TestNoFare(t *testing.T) {
	// Directions response, sans steps.
	response := `{
   "geocoded_waypoints" : [
      {
         "geocoder_status" : "OK",
         "place_id" : "ChIJt4ABNBBfrIkRXFPZnsnIicw",
         "types" : [ "street_address" ]
      },
      {
         "geocoder_status" : "OK",
         "place_id" : "EjExMDAxLTExOTkgSGlsbHNib3JvdWdoIFN0LCBSYWxlaWdoLCBOQyAyNzYwMywgVVNB",
         "types" : [ "street_address" ]
      }
   ],
   "routes" : [
      {
         "bounds" : {
            "northeast" : {
               "lat" : 35.782453,
               "lng" : -78.625349
            },
            "southwest" : {
               "lat" : 35.7766535,
               "lng" : -78.6548689
            }
         },
         "copyrights" : "Map data ©2016 Google",
         "legs" : [
            {
               "arrival_time" : {
                  "text" : "8:23am",
                  "time_zone" : "America/New_York",
                  "value" : 1476620634
               },
               "departure_time" : {
                  "text" : "8:01am",
                  "time_zone" : "America/New_York",
                  "value" : 1476619311
               },
               "distance" : {
                  "text" : "2.1 mi",
                  "value" : 3408
               },
               "duration" : {
                  "text" : "22 mins",
                  "value" : 1323
               },
               "end_address" : "1001-1199 Hillsborough St, Raleigh, NC 27603, USA",
               "end_location" : {
                  "lat" : 35.782453,
                  "lng" : -78.6548689
               },
               "start_address" : "822 E Hargett St, Raleigh, NC 27601, USA",
               "start_location" : {
                  "lat" : 35.7777912,
                  "lng" : -78.625349
               },
               "traffic_speed_entry" : [],
               "via_waypoint" : []
            }
         ],
         "summary" : "",
         "warnings" : [
            "Walking directions are in beta.    Use caution – This route may be missing sidewalks or pedestrian paths."
         ],
         "waypoint_order" : []
      }
   ],
   "status" : "OK"
}`

	server := mockServer(200, response)
	defer server.Close()
	c, _ := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL))
	r := &DirectionsRequest{
		Origin:      "35.7777111, -78.625354",
		Destination: "35.7826242, -78.6547025",
		Mode:        TravelModeTransit,
	}

	resp, err := c.Directions(context.Background(), r)

	if len(resp.Routes) != 1 {
		t.Fatalf("Expected length of response is 1, was %+v", len(resp.Routes))
	}
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}

	if resp.Routes[0].Fare != nil {
		t.Errorf("expected %+v, was %+v", nil, resp.Routes[0].Fare)
	}
}

// https://maps.googleapis.com/maps/api/directions/json?
// origin=Adelaide,SA
// &destination=Adelaide,SA
// &waypoints=optimize:true|Barossa+Valley,SA|Clare,SA|Connawarra,SA|McLaren+Vale,SA
// https://github.com/googlemaps/google-maps-services-go/issues/138
func TestConstructParamsWithOptimizeFlag(t *testing.T) {
	require := require.New(t)
	r := new(DirectionsRequest)
	r.Destination = "Adelaide,SA"
	r.Origin = "Adelaide,SA"
	r.Optimize = true
	r.Waypoints = []string{"Barossa+Valley,SA", "Clare,SA", "Connawarra,SA", "McLaren+Vale,SA"}
	v := r.params()
	uri, _ := url.QueryUnescape(v.Encode())
	require.Equal("destination=Adelaide,SA&origin=Adelaide,SA&waypoints=optimize:true|Barossa+Valley,SA|Clare,SA|Connawarra,SA|McLaren+Vale,SA", uri)
}

func TestConstructParamsWithoutOptimizeFlag(t *testing.T) {
	require := require.New(t)
	r := new(DirectionsRequest)
	r.Destination = "Adelaide,SA"
	r.Origin = "Adelaide,SA"
	r.Waypoints = []string{"Barossa+Valley,SA", "Clare,SA", "Connawarra,SA", "McLaren+Vale,SA"}
	v := r.params()
	uri, _ := url.QueryUnescape(v.Encode())
	require.Equal("destination=Adelaide,SA&origin=Adelaide,SA&waypoints=Barossa+Valley,SA|Clare,SA|Connawarra,SA|McLaren+Vale,SA", uri)
}
