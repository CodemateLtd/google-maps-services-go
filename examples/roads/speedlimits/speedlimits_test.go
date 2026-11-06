package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

func speedFixture(t *testing.T, handler http.HandlerFunc) []maps.ClientOption {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 2 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}
}

func runSpeed(t *testing.T, args []string, options ...maps.ClientOption) (int, error, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code, err := run(ctx, args, &stdout, &stderr, options...)
	return code, err, stdout.String(), stderr.String()
}

func TestRunSpeedInputSelectionUnitsAndIsolation(t *testing.T) {
	// Each run uses the same process and client options; no path, IDs, units or key
	// from an earlier invocation may leak into a later request.
	cases := []struct {
		args  []string
		path  string
		ids   []string
		units string
		key   string
	}{
		{[]string{"-key", "first-key", "-path", "1e1,2.50|-3.25,4", "-units", "MPH"}, "10,2.5|-3.25,4", nil, "MPH", "first-key"},
		{[]string{"-key", "second-key", "-place_ids", "road-a,road-b", "-units", "KPH"}, "", []string{"road-a", "road-b"}, "KPH", "second-key"},
		{[]string{"-key", "third-key", "-path", "7,8", "-place_ids", "road-c,road-d"}, "7,8", []string{"road-c", "road-d"}, "", "third-key"},
		{[]string{"-key", "fourth-key", "-place_ids", "road-e"}, "", []string{"road-e"}, "", "fourth-key"},
	}
	var calls int32
	options := speedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		index := int(atomic.AddInt32(&calls, 1)) - 1
		if index >= len(cases) {
			t.Errorf("unexpected extra request: %s", r.URL)
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		want := cases[index]
		q := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Path != "/v1/speedLimits" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if q.Get("path") != want.path || !reflect.DeepEqual(q["placeId"], want.ids) || q.Get("units") != want.units || q.Get("key") != want.key {
			t.Errorf("request %d query = %v; want path %q, IDs %v, units %q, key %q", index, q, want.path, want.ids, want.units, want.key)
		}
		if want.path == "" && len(q["path"]) != 0 {
			t.Errorf("place-ID-only request should omit path: %v", q)
		}
		if want.units == "" && len(q["units"]) != 0 {
			t.Errorf("default units should be omitted: %v", q)
		}
		fmt.Fprint(w, `{"speedLimits":[{"placeId":"fixture-express-road","speedLimit":88.5,"units":"KPH"},{"placeId":"fixture-town-road","speedLimit":25,"units":"MPH"}],"snappedPoints":[{"location":{"latitude":-35.125,"longitude":149.75},"originalIndex":3,"placeId":"fixture-express-road"}]}`)
	})
	for _, tc := range cases {
		code, err, stdout, stderr := runSpeed(t, tc.args, options...)
		if code != 0 || err != nil || stderr != "" {
			t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
		}
		for _, pattern := range []string{
			`PlaceID:\s*"fixture-express-road"`, `PlaceID:\s*"fixture-town-road"`,
			`SpeedLimit:\s*88\.5\b`, `SpeedLimit:\s*25\b`,
			`Units:\s*"KPH"`, `Units:\s*"MPH"`,
			`Lat:\s*-35\.125\b`, `Lng:\s*149\.75\b`, `OriginalIndex:[^\n]*3`,
		} {
			if !regexp.MustCompile(pattern).MatchString(stdout) {
				t.Errorf("decoded output missing %q:\n%s", pattern, stdout)
			}
		}
	}
	if got := atomic.LoadInt32(&calls); got != int32(len(cases)) {
		t.Errorf("HTTP calls = %d, want %d", got, len(cases))
	}
}

func TestRunSpeedValidationAndHelpAvoidHTTP(t *testing.T) {
	var calls int32
	options := speedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, "unexpected request", http.StatusBadRequest)
	})
	cases := []struct {
		name       string
		args       []string
		code       int
		parseError bool
	}{
		{"help", []string{"-help"}, 0, false},
		{"missing key", []string{"-place_ids", "road-a"}, 2, false},
		{"missing inputs", []string{"-key", "test-key"}, 2, false},
		{"unknown flag", []string{"-unknown"}, 2, true},
		{"legacy credentials", []string{"-client_id", "legacy", "-signature", "secret"}, 2, true},
		{"invalid units", []string{"-key", "test-key", "-place_ids", "road-a", "-units", "knots"}, 2, false},
		{"lowercase units", []string{"-key", "test-key", "-path", "1,2", "-units", "mph"}, 2, false},
		{"positional argument", []string{"-key", "test-key", "-place_ids", "road-a", "extra"}, 2, false},
		{"leading empty ID", []string{"-key", "test-key", "-place_ids", ",road-a"}, 2, false},
		{"trailing empty ID", []string{"-key", "test-key", "-place_ids", "road-a,"}, 2, false},
		{"middle empty ID", []string{"-key", "test-key", "-place_ids", "road-a,,road-b"}, 2, false},
		{"blank ID with valid path", []string{"-key", "test-key", "-place_ids", " ", "-path", "1,2"}, 2, false},
	}
	for _, path := range []string{"1", "1,2,3", ",2", "1,", "bad,2", "1,bad", "1,2|", "|1,2", "1,2||3,4", "NaN,2", "1,-Inf", "1e999,2"} {
		cases = append(cases, struct {
			name       string
			args       []string
			code       int
			parseError bool
		}{path, []string{"-key", "test-key", "-path", path, "-place_ids", "valid-road"}, 2, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err, stdout, stderr := runSpeed(t, tc.args, options...)
			if code != tc.code || stdout != "" {
				t.Fatalf("run = (%d, %v), stdout %q", code, err, stdout)
			}
			if tc.code == 0 && err != nil {
				t.Errorf("help error = %v", err)
			}
			if tc.code == 2 && !tc.parseError && err == nil {
				t.Error("validation did not return an error")
			}
			if tc.parseError && (err != nil || stderr == "") {
				t.Errorf("parse result error = %v, diagnostic = %q", err, stderr)
			}
		})
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Errorf("invalid/help invocations made %d requests", got)
	}
}

func TestRunSpeedServiceResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		wantCode int
	}{
		{"forbidden", 403, `{"error":{"code":403,"message":"speed permission denied","status":"PERMISSION_DENIED"}}`, 1},
		{"malformed JSON", 200, `{"speedLimits":`, 1},
		{"empty success", 200, `{"speedLimits":[],"snappedPoints":[]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			options := speedFixture(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			code, err, stdout, stderr := runSpeed(t, []string{"-key", "test-key", "-place_ids", "road-a"}, options...)
			if code != tc.wantCode || stderr != "" {
				t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
			}
			if tc.wantCode == 1 {
				if err == nil || stdout != "" {
					t.Fatalf("failure error = %v, stdout %q", err, stdout)
				}
			} else {
				if err != nil {
					t.Errorf("empty success error = %v", err)
				}
			}
			if tc.name == "malformed JSON" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("SDK JSON decode error = %v, want unexpected EOF", err)
			}
			if tc.status == 403 {
				var httpError *maps.HTTPError
				if !errors.As(err, &httpError) || httpError.StatusCode != 403 || httpError.Code != 403 || httpError.Message != "speed permission denied" || httpError.Status != "PERMISSION_DENIED" {
					t.Errorf("SDK error envelope = %#v", err)
				}
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("HTTP calls = %d, want 1", got)
			}
		})
	}
}

type speedFailWriter struct{ err error }

func (w speedFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunSpeedOutputClientAndCancellationFailures(t *testing.T) {
	var calls int32
	options := speedFixture(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"speedLimits":[],"snappedPoints":[]}`)
	})
	args := []string{"-key", "test-key", "-place_ids", "road-a"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	writeErr := errors.New("speed output unavailable")
	code, err := run(ctx, args, speedFailWriter{writeErr}, new(bytes.Buffer), options...)
	if code != 1 || !errors.Is(err, writeErr) {
		t.Errorf("output failure = (%d, %v)", code, err)
	}
	clientErr := errors.New("speed client unavailable")
	badOptions := append(append([]maps.ClientOption(nil), options...), func(*maps.Client) error { return clientErr })
	code, err, stdout, _ := runSpeed(t, args, badOptions...)
	if code != 1 || !errors.Is(err, clientErr) || stdout != "" {
		t.Errorf("client failure = (%d, %v), stdout %q", code, err, stdout)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	code, err = run(canceled, args, new(bytes.Buffer), new(bytes.Buffer), options...)
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Errorf("canceled run = (%d, %v)", code, err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("HTTP calls = %d, want only the output-failure request", got)
	}
}
