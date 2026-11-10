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
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"time"
)

const (
	jsonSnappedPoint = `{"originalIndex":null,"placeId":"helloPlace","location":{"latitude":-33.870315,"longitude":151.196532}}`
)

func TestSnappedPoint(t *testing.T) {
	sp := SnappedPoint{
		Location: LatLng{
			Lat: -33.870315,
			Lng: 151.196532,
		},
		PlaceID: "helloPlace",
	}

	bytes, err := json.Marshal(&sp)
	if err != nil {
		t.Errorf("expected ok encode of SnappedPoint, got: %v", err)
	}
	if string(bytes) != jsonSnappedPoint {
		t.Errorf("expected encoded snappedPoint, was: %v", string(bytes))
	}

	var out SnappedPoint
	err = json.Unmarshal(bytes, &out)
	if err != nil {
		t.Errorf("expected ok decode of SnappedPoint, got: %v", err)
	}
	if !reflect.DeepEqual(out, sp) {
		t.Errorf("expected equal snappedPoint, was %+v expected %+v", out, sp)
	}
}

func TestDistanceMatrixElement_MarshalJSON(t *testing.T) {
	dme := &DistanceMatrixElement{
		Duration:          1 * time.Second,
		DurationInTraffic: 2 * time.Second,
	}
	b, err := dme.MarshalJSON()
	if err != nil {
		t.Errorf("expected ok encode of DistanceMatrixElement, got: %v", err)
	}

	out := &DistanceMatrixElement{}
	err = out.UnmarshalJSON(b)
	if err != nil {
		t.Errorf("expected ok decode of DistanceMatrixElement, got: %v", err)
	}
	if !reflect.DeepEqual(dme, out) {
		t.Errorf("expected equal DistanceMatrixElement, was %+v expected %+v", out, dme)
	}
}

func TestStepManeuverEncoding(t *testing.T) {
	var step Step
	err := json.Unmarshal([]byte(`{
		"duration": {"value": 45, "text": "45 seconds"},
		"maneuver": "turn-left",
		"steps": [{
			"duration": {"value": 12, "text": "12 seconds"},
			"maneuver": "future-maneuver"
		}]
	}`), &step)
	if err != nil {
		t.Fatal(err)
	}
	if step.Maneuver != "turn-left" || step.Duration != 45*time.Second {
		t.Errorf("Unexpected decoded step: %+v", step)
	}
	if len(step.Steps) != 1 {
		t.Fatalf("Expected one nested step, got %+v", step.Steps)
	}
	if nested := step.Steps[0]; nested.Maneuver != "future-maneuver" || nested.Duration != 12*time.Second {
		t.Errorf("Unexpected decoded nested step: %+v", nested)
	}

	data, err := json.Marshal(&step)
	if err != nil {
		t.Fatal(err)
	}
	type wireStep struct {
		Maneuver string `json:"maneuver"`
		Duration struct {
			Value int64 `json:"value"`
		} `json:"duration"`
		Steps []wireStep `json:"steps"`
	}
	var wire wireStep
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Maneuver != "turn-left" || wire.Duration.Value != 45 {
		t.Errorf("Unexpected encoded step: %+v", wire)
	}
	if len(wire.Steps) != 1 {
		t.Fatalf("Expected one encoded nested step, got %+v", wire.Steps)
	}
	if nested := wire.Steps[0]; nested.Maneuver != "future-maneuver" || nested.Duration.Value != 12 {
		t.Errorf("Unexpected encoded nested step: %+v", nested)
	}
}

func TestStepAbsentManeuver(t *testing.T) {
	var step Step
	if err := json.Unmarshal([]byte(`{}`), &step); err != nil {
		t.Fatal(err)
	}
	if step.Maneuver != "" || step.Duration != 0 {
		t.Errorf("Expected zero values for omitted fields, got %+v", step)
	}
	data, err := json.Marshal(&step)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, present := wire["maneuver"]; present {
		t.Errorf("Expected absent maneuver to remain omitted, got %s", data)
	}
}

func TestTransitLineVehicleLocalIconEncoding(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		icon      string
		localIcon string
	}{
		{
			name:      "both icons",
			input:     `{"icon":"https://example.com/bus.png","local_icon":"https://example.com/local-bus.png"}`,
			icon:      "https://example.com/bus.png",
			localIcon: "https://example.com/local-bus.png",
		},
		{
			name:      "local icon only",
			input:     `{"local_icon":"https://example.com/local-bus.png"}`,
			localIcon: "https://example.com/local-bus.png",
		},
		{
			name:  "absent local icon",
			input: `{"icon":"https://example.com/bus.png"}`,
			icon:  "https://example.com/bus.png",
		},
		{
			name:  "absent icons",
			input: `{}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vehicle TransitLineVehicle
			if err := json.Unmarshal([]byte(tc.input), &vehicle); err != nil {
				t.Fatal(err)
			}
			if vehicle.Icon == nil || vehicle.Icon.String() != tc.icon {
				t.Errorf("Unexpected icon: %+v", vehicle.Icon)
			}
			if tc.localIcon == "" {
				if vehicle.LocalIcon != nil {
					t.Errorf("Expected absent local icon to be nil, got %+v", vehicle.LocalIcon)
				}
			} else if vehicle.LocalIcon == nil || vehicle.LocalIcon.String() != tc.localIcon {
				t.Errorf("Unexpected local icon: %+v", vehicle.LocalIcon)
			}

			data, err := json.Marshal(&vehicle)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Icon      string `json:"icon"`
				LocalIcon string `json:"local_icon"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Icon != tc.icon || wire.LocalIcon != tc.localIcon {
				t.Errorf("Unexpected encoded icons: %+v", wire)
			}
			if tc.localIcon == "" {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(data, &fields); err != nil {
					t.Fatal(err)
				}
				if _, present := fields["local_icon"]; present {
					t.Errorf("Expected absent local icon to remain omitted, got %s", data)
				}
			}
		})
	}
}

func TestTransitLineVehicleMalformedIconURLs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
	}{
		{
			name:  "icon",
			input: `{"icon":"https://example.com/%zz"}`,
		},
		{
			name:  "local icon",
			input: `{"icon":"https://example.com/bus.png","local_icon":"https://example.com/%zz"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vehicle TransitLineVehicle
			err := json.Unmarshal([]byte(tc.input), &vehicle)
			urlError, ok := err.(*url.Error)
			if !ok {
				t.Fatalf("Expected URL parse error, got %T: %v", err, err)
			}
			if urlError.Op != "parse" || urlError.URL != "https://example.com/%zz" {
				t.Errorf("Unexpected URL parse error details: %+v", urlError)
			}
		})
	}
}

func TestDirectionsResultNestedTransitURLs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		details     string
		lineURL     string
		lineIcon    string
		vehicleIcon string
		localIcon   string
	}{
		{
			name:        "all URLs",
			details:     `{"line":{"url":"https://example.com/line","icon":"https://example.com/line.png","vehicle":{"icon":"https://example.com/bus.png","local_icon":"https://example.com/local-bus.png"}}}`,
			lineURL:     "https://example.com/line",
			lineIcon:    "https://example.com/line.png",
			vehicleIcon: "https://example.com/bus.png",
			localIcon:   "https://example.com/local-bus.png",
		},
		{
			name:      "local icon only",
			details:   `{"line":{"vehicle":{"local_icon":"https://example.com/local-bus.png"}}}`,
			localIcon: "https://example.com/local-bus.png",
		},
		{
			name:        "local icon omitted",
			details:     `{"line":{"vehicle":{"icon":"https://example.com/bus.png"}}}`,
			vehicleIcon: "https://example.com/bus.png",
		},
		{
			name:    "vehicle omitted",
			details: `{"line":{"name":"Walking transfer"}}`,
		},
		{
			name:    "line omitted",
			details: `{}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := `{"routes":[{"legs":[{"steps":[{"transit_details":` + tc.details + `}]}]}]}`
			var result DirectionsResult
			if err := json.Unmarshal([]byte(input), &result); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Routes []struct {
					Legs []struct {
						Steps []struct {
							TransitDetails struct {
								Line struct {
									URL     string `json:"url"`
									Icon    string `json:"icon"`
									Vehicle struct {
										Icon      string  `json:"icon"`
										LocalIcon *string `json:"local_icon"`
									} `json:"vehicle"`
								} `json:"line"`
							} `json:"transit_details"`
						} `json:"steps"`
					} `json:"legs"`
				} `json:"routes"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatalf("Nested URLs must be encoded as API strings: %v", err)
			}
			line := wire.Routes[0].Legs[0].Steps[0].TransitDetails.Line
			if line.URL != tc.lineURL || line.Icon != tc.lineIcon || line.Vehicle.Icon != tc.vehicleIcon {
				t.Errorf("Unexpected encoded transit URLs: %+v", line)
			}
			if tc.localIcon == "" {
				if line.Vehicle.LocalIcon != nil {
					t.Errorf("Expected omitted local icon, got %q", *line.Vehicle.LocalIcon)
				}
			} else if line.Vehicle.LocalIcon == nil || *line.Vehicle.LocalIcon != tc.localIcon {
				t.Errorf("Expected local icon %q, got %+v", tc.localIcon, line.Vehicle.LocalIcon)
			}
			var decoded DirectionsResult
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Encoded result must be readable by the SDK: %v", err)
			}
			localIcon := decoded.Routes[0].Legs[0].Steps[0].TransitDetails.Line.Vehicle.LocalIcon
			if tc.localIcon == "" {
				if localIcon != nil {
					t.Errorf("Expected absent decoded local icon, got %v", localIcon)
				}
			} else if localIcon == nil || localIcon.String() != tc.localIcon {
				t.Errorf("Expected decoded local icon %q, got %v", tc.localIcon, localIcon)
			}
		})
	}
}
