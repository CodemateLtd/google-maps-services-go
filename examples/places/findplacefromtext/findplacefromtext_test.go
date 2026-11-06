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

const candidateResponse = `{"status":"OK","candidates":[{"name":"Harbor Museum","place_id":"museum-73","formatted_address":"9 Dock Street","rating":4.75,"geometry":{"location":{"lat":37.25,"lng":-122.5}}}],"html_attributions":["Museum attribution"]}`

var findArgs = []string{"-key=fixture", "-input=Harbor Museum", "-inputtype=textquery"}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func fixtureRun(t *testing.T, args []string, response string, writer io.Writer) (int, error, string, []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var requests []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/maps/api/place/findplacefromtext/json" {
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

func TestRunLocationBiasFieldsAndIsolation(t *testing.T) {
	cases := []struct {
		name                           string
		extra                          []string
		bias, fields, inputType, input string
	}{
		{"point and masks", []string{"-locationbias=point", "-point=37.25,-122.5", "-fields=name,place_id,geometry"}, "point:37.25,-122.5", "name,place_id,geometry", "textquery", "Harbor Museum"},
		{"circle", []string{"-locationbias=circle", "-center=1.5,-2.25", "-radius=500"}, "circle:500@1.5,-2.25", "", "textquery", "Harbor Museum"},
		{"rectangle", []string{"-locationbias=rectangle", "-southwest=-1,-2", "-northeast=3,4"}, "rectangle:-1,-2|3,4", "", "textquery", "Harbor Museum"},
		{"ip bias", []string{"-locationbias=ipbias"}, "ipbias", "", "textquery", "Harbor Museum"},
		{"phone", []string{"-inputtype=phonenumber", "-input=+16505550123"}, "", "", "phonenumber", "+16505550123"},
		{"repeat defaults", nil, "", "", "textquery", "Harbor Museum"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			args := append(append([]string{}, findArgs...), tt.extra...)
			code, err, output, requests := fixtureRun(t, args, candidateResponse, nil)
			if code != 0 || err != nil || len(requests) != 1 {
				t.Fatalf("run = %d, %v, %v", code, err, requests)
			}
			for key, want := range map[string]string{"locationbias": tt.bias, "fields": tt.fields, "inputtype": tt.inputType, "input": tt.input, "key": "fixture"} {
				if got := requests[0].Get(key); got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
				if want == "" {
					if _, present := requests[0][key]; present {
						t.Errorf("unexpected %s in %v", key, requests[0])
					}
				}
			}
			for _, want := range []string{"Harbor Museum", "museum-73", "9 Dock Street", "4.75", "37.25", "-122.5", "Museum attribution"} {
				if !strings.Contains(output, want) {
					t.Errorf("decoded output missing %q: %s", want, output)
				}
			}
		})
	}
}

func TestRunRejectsInvalidInputBeforeHTTP(t *testing.T) {
	cases := [][]string{
		{"-input="}, {"-inputtype="}, {"-inputtype=invalid"}, {"-fields=name,not-a-field"}, {"-fields=name,"},
		{"-locationbias=invalid"}, {"-locationbias=point"}, {"-locationbias=point", "-point=1"},
		{"-locationbias=point", "-point=1,2,3"}, {"-locationbias=point", "-point=bad,2"},
		{"-locationbias=circle", "-center=1,no", "-radius=100"}, {"-locationbias=circle", "-center=1,2"},
		{"-locationbias=circle", "-center=1,2", "-radius=not-a-number"},
		{"-locationbias=rectangle", "-southwest=1,2"},
		{"-locationbias=rectangle", "-southwest=1", "-northeast=3,4"},
		{"-locationbias=rectangle", "-southwest=1,2", "-northeast=no,4"},
		{"-unknown"}, {"extra"}, {"-key="}, {"-client_id=gme-fixture"}, {"-signature=c2VjcmV0"},
	}
	for _, extra := range cases {
		args := append(append([]string{}, findArgs...), extra...)
		code, _, output, requests := fixtureRun(t, args, candidateResponse, nil)
		if code != 2 || output != "" || len(requests) != 0 {
			t.Errorf("%v: code=%d output=%q requests=%v", extra, code, output, requests)
		}
	}
	code, err, _, requests := fixtureRun(t, []string{"-help"}, candidateResponse, nil)
	if code != 0 || err != nil || len(requests) != 0 {
		t.Errorf("help = %d, %v, %v", code, err, requests)
	}
}

func TestRunFailureBoundaries(t *testing.T) {
	for _, response := range []string{`{"status":"REQUEST_DENIED","error_message":"fixture denied"}`, `not json`} {
		code, err, output, requests := fixtureRun(t, findArgs, response, nil)
		if code != 1 || err == nil || output != "" || len(requests) != 1 {
			t.Errorf("service failure = %d, %v, %q, %v", code, err, output, requests)
		}
		if strings.Contains(response, "fixture denied") && (err == nil || !strings.Contains(err.Error(), "fixture denied")) {
			t.Errorf("service error lost: %v", err)
		}
	}
	code, err, _, _ := fixtureRun(t, findArgs, candidateResponse, brokenWriter{})
	if code != 1 || err == nil || !strings.Contains(err.Error(), "output unavailable") {
		t.Errorf("output failure = %d, %v", code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	code, err = run(ctx, findArgs, &output, &output, maps.WithBaseURL("http://127.0.0.1:1"))
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled request = %d, %v", code, err)
	}
}
