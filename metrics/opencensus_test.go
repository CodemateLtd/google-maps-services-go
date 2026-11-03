package metrics_test

import (
	"context"
	"errors"
	"testing"

	"go.opencensus.io/stats/view"
	"googlemaps.github.io/maps"
	"googlemaps.github.io/maps/metrics"
)

func TestClientWithOpenCensus(t *testing.T) {
	if err := metrics.RegisterViews(); err != nil {
		t.Fatal(err)
	}
	defer view.Unregister(metrics.Count, metrics.Latency)
	server := mockServer([]int{200, 400}, `{"results" : [], "status" : "OK"}`)
	defer server.Close()
	c, err := maps.NewClient(
		maps.WithAPIKey("AIza-Maps-API-Key"),
		maps.WithBaseURL(server.URL),
		maps.WithMetricReporter(metrics.OpenCensusReporter{}))
	if err != nil {
		t.Errorf("Unable to create client with OpenCensusReporter")
	}
	r := &maps.ElevationRequest{
		Locations: []maps.LatLng{
			{
				Lat: 39.73915360,
				Lng: -104.9847034,
			},
		},
	}
	_, err = c.Elevation(context.Background(), r)
	if err != nil {
		t.Errorf("r.Get returned non nil error, was %+v", err)
	}
	_, err = c.Elevation(context.Background(), r)
	var httpErr *maps.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != 400 {
		t.Fatalf("expected HTTP 400 error, got %T: %v", err, err)
	}
	rows, err := view.RetrieveData(metrics.Count.Name)
	if err != nil {
		t.Fatal(err)
	}
	wantCounts := map[string]int64{"200": 1, "400": 1}
	for _, row := range rows {
		var httpCode, apiStatus string
		for _, tag := range row.Tags {
			switch tag.Key.Name() {
			case "http_code":
				httpCode = tag.Value
			case "api_status":
				apiStatus = tag.Value
			}
		}
		count, ok := row.Data.(*view.CountData)
		if !ok || count.Value != wantCounts[httpCode] {
			t.Fatalf("unexpected count for HTTP %s: %+v", httpCode, row.Data)
		}
		if (apiStatus != "") != (httpCode != "200") {
			t.Errorf("HTTP %s recorded the wrong success/failure classification: %q", httpCode, apiStatus)
		}
		delete(wantCounts, httpCode)
	}
	if len(wantCounts) != 0 {
		t.Errorf("missing HTTP request counts: %v", wantCounts)
	}
}
