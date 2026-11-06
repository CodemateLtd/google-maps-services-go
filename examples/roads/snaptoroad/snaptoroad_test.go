package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"googlemaps.github.io/maps"
)

func snapFixture(t *testing.T, handler http.HandlerFunc) []maps.ClientOption {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 2 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return []maps.ClientOption{maps.WithBaseURL(server.URL), maps.WithHTTPClient(client)}
}

func runSnap(t *testing.T, args []string, options ...maps.ClientOption) (int, error, string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code, err := run(ctx, args, &stdout, &stderr, options...)
	return code, err, stdout.String(), stderr.String()
}

func TestRunSnapPathInterpolationAndDecodedPoints(t *testing.T) {
	var calls int32
	options := snapFixture(t, func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/snapToRoads" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if call == 1 {
			if q.Get("path") != "10,2.5|-3.25,4" || q.Get("interpolate") != "true" || q.Get("key") != "first-key" {
				t.Errorf("first request query = %v", q)
			}
		} else if q.Get("path") != "5,6" || len(q["interpolate"]) != 0 || q.Get("key") != "second-key" {
			t.Errorf("independent request query = %v", q)
		}
		fmt.Fprint(w, `{"snappedPoints":[{"location":{"latitude":-35.125,"longitude":149.75},"originalIndex":7,"placeId":"fixture-original-road"},{"location":{"latitude":-35.25,"longitude":149.875},"placeId":"fixture-interpolated-road"}]}`)
	})
	for _, args := range [][]string{
		{"-key", "first-key", "-path", "1e1,2.50|-3.25,4", "-interpolate"},
		{"-key", "second-key", "-path", "5,6"},
	} {
		code, err, stdout, stderr := runSnap(t, args, options...)
		if code != 0 || err != nil || stderr != "" {
			t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
		}
		for _, pattern := range []string{
			`Lat:\s*-35\.125\b`, `Lng:\s*149\.75\b`,
			`Lat:\s*-35\.25\b`, `Lng:\s*149\.875\b`,
			`OriginalIndex:[^\n]*7`, `OriginalIndex:[^\n]*nil`,
			`PlaceID:\s*"fixture-original-road"`, `PlaceID:\s*"fixture-interpolated-road"`,
		} {
			if !regexp.MustCompile(pattern).MatchString(stdout) {
				t.Errorf("decoded output missing %q:\n%s", pattern, stdout)
			}
		}
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("HTTP calls = %d, want 2", got)
	}
}

func TestRunSnapValidationAndHelpAvoidHTTP(t *testing.T) {
	var calls int32
	options := snapFixture(t, func(w http.ResponseWriter, r *http.Request) {
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
		{"missing key", []string{"-path", "1,2"}, 2, false},
		{"missing path", []string{"-key", "test-key"}, 2, false},
		{"unknown flag", []string{"-unknown"}, 2, true},
		{"legacy credentials", []string{"-client_id", "legacy", "-signature", "secret"}, 2, true},
		{"invalid boolean", []string{"-interpolate=perhaps"}, 2, true},
		{"positional argument", []string{"-key", "test-key", "-path", "1,2", "extra"}, 2, false},
	}
	for _, path := range []string{"1", "1,2,3", ",2", "1,", "bad,2", "1,bad", "1,2|", "|1,2", "1,2||3,4", "NaN,2", "1,+Inf", "1e999,2"} {
		cases = append(cases, struct {
			name       string
			args       []string
			code       int
			parseError bool
		}{path, []string{"-key", "test-key", "-path", path}, 2, false})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err, stdout, stderr := runSnap(t, tc.args, options...)
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

func TestRunSnapServiceResponses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		wantCode int
	}{
		{"forbidden", 403, `{"error":{"code":403,"message":"snap permission denied","status":"PERMISSION_DENIED"}}`, 1},
		{"malformed JSON", 200, `{"snappedPoints":`, 1},
		{"empty success", 200, `{"snappedPoints":[]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			options := snapFixture(t, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			code, err, stdout, stderr := runSnap(t, []string{"-key", "test-key", "-path", "1,2"}, options...)
			if code != tc.wantCode || stderr != "" {
				t.Fatalf("run = (%d, %v), stderr %q", code, err, stderr)
			}
			if tc.wantCode == 1 {
				if err == nil || stdout != "" {
					t.Fatalf("failure error = %v, stdout %q", err, stdout)
				}
			} else if err != nil {
				t.Errorf("empty success error = %v", err)
			}
			if tc.name == "malformed JSON" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("SDK JSON decode error = %v, want unexpected EOF", err)
			}
			if tc.status == 403 {
				var httpError *maps.HTTPError
				if !errors.As(err, &httpError) || httpError.StatusCode != 403 || httpError.Code != 403 || httpError.Message != "snap permission denied" || httpError.Status != "PERMISSION_DENIED" {
					t.Errorf("SDK error envelope = %#v", err)
				}
			}
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("HTTP calls = %d, want 1", got)
			}
		})
	}
}

type snapFailWriter struct{ err error }

func (w snapFailWriter) Write([]byte) (int, error) { return 0, w.err }

func TestRunSnapOutputClientAndCancellationFailures(t *testing.T) {
	var calls int32
	options := snapFixture(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		fmt.Fprint(w, `{"snappedPoints":[]}`)
	})
	args := []string{"-key", "test-key", "-path", "1,2"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	writeErr := errors.New("snap output unavailable")
	code, err := run(ctx, args, snapFailWriter{writeErr}, new(bytes.Buffer), options...)
	if code != 1 || !errors.Is(err, writeErr) {
		t.Errorf("output failure = (%d, %v)", code, err)
	}
	clientErr := errors.New("snap client unavailable")
	badOptions := append(append([]maps.ClientOption(nil), options...), func(*maps.Client) error { return clientErr })
	code, err, stdout, _ := runSnap(t, args, badOptions...)
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
