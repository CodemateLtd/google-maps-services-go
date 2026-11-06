package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

const searchResponse = `{"status":"OK","results":[{"name":"Harbor Cafe","place_id":"harbor-42","rating":4.5,"geometry":{"location":{"lat":12.25,"lng":-45.5}},"formatted_address":"7 Pier Lane"}],"next_page_token":"page-two"}`

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func fixtureRun(t *testing.T, args []string, response string, writer io.Writer) (int, error, string, []url.Values) {
	t.Helper()
	var requests []url.Values
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/maps/api/place/nearbysearch/json" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		mu.Lock()
		requests = append(requests, r.URL.Query())
		mu.Unlock()
		fmt.Fprint(w, response)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var output, diagnostics bytes.Buffer
	if writer == nil {
		writer = &output
	}
	code, err := run(ctx, args, writer, &diagnostics, maps.WithBaseURL(server.URL), maps.WithHTTPClient(server.Client()))
	mu.Lock()
	defer mu.Unlock()
	return code, err, output.String(), requests
}

func TestRunPriceBounds(t *testing.T) {
	cases := []struct{ name, min, max string }{
		{"omitted", "", ""}, {"minimum only", "2", ""}, {"free minimum", "0", ""},
		{"free maximum", "", "0"}, {"inexpensive maximum", "", "1"}, {"moderate maximum", "", "2"},
		{"expensive maximum", "", "3"}, {"very expensive maximum", "", "4"}, {"distinct bounds", "1", "4"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{}, validArgs...)
			if tt.min != "" {
				args = append(args, "-minprice="+tt.min)
			}
			if tt.max != "" {
				args = append(args, "-maxprice="+tt.max)
			}
			code, err, output, requests := fixtureRun(t, args, searchResponse, nil)
			if code != 0 || err != nil {
				t.Fatalf("run = %d, %v", code, err)
			}
			if len(requests) != 1 {
				t.Fatalf("requests = %v", requests)
			}
			for key, want := range map[string]string{"minprice": tt.min, "maxprice": tt.max} {
				got, present := requests[0][key]
				if present != (want != "") || (present && (len(got) != 1 || got[0] != want)) {
					t.Errorf("%s = %v (present %v), want %q", key, got, present, want)
				}
			}
			for _, want := range []string{"Harbor Cafe", "harbor-42", "4.5", "12.25", "-45.5", "page-two"} {
				if !strings.Contains(output, want) {
					t.Errorf("decoded output missing %q: %s", want, output)
				}
			}
		})
	}
}

func TestRunInvalidInputAndHelp(t *testing.T) {
	cases := [][]string{
		{"-minprice=-1"}, {"-minprice=5"}, {"-maxprice=5"}, {"-minprice=1", "-maxprice=not-a-price"},
		{"-location=1"}, {"-location=1,no"}, {"-location=1,2,3"}, {"-type=not-a-place-type"}, {"-radius=-1"}, {"-unknown"}, {"extra"},
		{"-rankby=invalid"}, {"-rankby=distance"},
	}
	for _, extra := range cases {
		args := append(append([]string{}, validArgs...), extra...)
		code, _, output, requests := fixtureRun(t, args, searchResponse, nil)
		if code != 2 || len(requests) != 0 || output != "" {
			t.Errorf("%v: code=%d output=%q requests=%v", extra, code, output, requests)
		}
	}
	code, err, _, requests := fixtureRun(t, []string{"-help"}, searchResponse, nil)
	if code != 0 || err != nil || len(requests) != 0 {
		t.Fatalf("help = %d, %v, %v", code, err, requests)
	}
}

func TestRunErrorBoundaries(t *testing.T) {
	for _, response := range []string{`{"status":"REQUEST_DENIED","error_message":"fixture denied"}`, `not json`} {
		code, err, output, requests := fixtureRun(t, validArgs, response, nil)
		if code != 1 || err == nil || output != "" || len(requests) != 1 {
			t.Errorf("service failure = %d, %v, %q, %v", code, err, output, requests)
		}
		if strings.Contains(response, "fixture denied") && (err == nil || !strings.Contains(err.Error(), "fixture denied")) {
			t.Errorf("service error lost: %v", err)
		}
	}
	code, err, _, _ := fixtureRun(t, validArgs, searchResponse, brokenWriter{})
	if code != 1 || err == nil || !strings.Contains(err.Error(), "output unavailable") {
		t.Errorf("write failure = %d, %v", code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	code, err = run(ctx, validArgs, &output, &output, maps.WithBaseURL("http://127.0.0.1:1"))
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled request = %d, %v", code, err)
	}
}

var validArgs = []string{"-key=fixture", "-location=1,2", "-radius=100"}

func TestRunSearchSelectionAndIsolation(t *testing.T) {
	cases := []struct {
		args   []string
		want   map[string]string
		absent []string
	}{
		{[]string{"-key=fixture", "-location=12.5,-4", "-rankby=distance", "-type=cafe", "-keyword=tea", "-language=fr", "-open_now", "-name=Dock"}, map[string]string{"location": "12.5,-4", "rankby": "distance", "type": "cafe", "keyword": "tea", "language": "fr", "opennow": "true", "name": "Dock"}, []string{"radius"}},
		{validArgs, map[string]string{"location": "1,2", "radius": "100"}, []string{"rankby", "type", "keyword", "language", "opennow", "name", "minprice", "maxprice"}},
		{[]string{"-key=fixture", "-pagetoken=next-page"}, map[string]string{"pagetoken": "next-page"}, []string{"location", "radius"}},
		{append(append([]string{}, validArgs...), "-rankby=prominence"), map[string]string{"rankby": "prominence"}, nil},
	}
	for _, tt := range cases {
		code, err, _, requests := fixtureRun(t, tt.args, searchResponse, nil)
		if code != 0 || err != nil || len(requests) != 1 {
			t.Fatalf("run = %d, %v, %v", code, err, requests)
		}
		for key, want := range tt.want {
			if requests[0].Get(key) != want {
				t.Errorf("%s = %q, want %q", key, requests[0].Get(key), want)
			}
		}
		for _, key := range tt.absent {
			if _, ok := requests[0][key]; ok {
				t.Errorf("unexpected %s in %v", key, requests[0])
			}
		}
	}
	for _, args := range [][]string{{"-key=fixture"}, {"-key=fixture", "-location=1,2"}, {"-key=fixture", "-location=1,2", "-rankby=distance"}} {
		code, _, _, requests := fixtureRun(t, args, searchResponse, nil)
		if code != 2 || len(requests) != 0 {
			t.Errorf("missing required input: %d, %v", code, requests)
		}
	}
}
