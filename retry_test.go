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

package maps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type retryTestTransport func(*http.Request) (*http.Response, error)

func (f retryTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type retryTestBody struct {
	io.Reader
	reads   int32
	closes  int32
	onClose func()
}

func (b *retryTestBody) Read(p []byte) (int, error) {
	atomic.AddInt32(&b.reads, 1)
	return b.Reader.Read(p)
}

func (b *retryTestBody) Close() error {
	if atomic.AddInt32(&b.closes, 1) == 1 && b.onClose != nil {
		b.onClose()
	}
	return nil
}

func retryTestResponse(status int, body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: body}
}

func retryTestPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, MaxElapsedTime: 2 * time.Second, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond}
}

func retryTestClient(t *testing.T, transport http.RoundTripper, options ...ClientOption) *Client {
	options = append([]ClientOption{WithAPIKey(apiKey), WithHTTPClient(&http.Client{Transport: transport}), WithRateLimit(0)}, options...)
	c, err := NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func retryTestGeocode(c *Client, ctx context.Context) error {
	_, err := c.Geocode(ctx, &GeocodingRequest{Address: "Sydney"})
	return err
}

func retryTestReadAll(r io.Reader) ([]byte, error) {
	var b bytes.Buffer
	_, err := b.ReadFrom(r)
	return b.Bytes(), err
}

func retryTestWait(t *testing.T, ch <-chan struct{}) {
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not reach synchronization point")
	}
}

func retryTestResult(t *testing.T, ch <-chan error) error {
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not finish")
		return nil
	}
}

func retryTestContextError(err error) error {
	if e, ok := err.(*url.Error); ok {
		return e.Err
	}
	return err
}

func TestRetryPolicyValidation(t *testing.T) {
	valid := retryTestPolicy()
	invalid := []RetryPolicy{
		{},
		{0, valid.MaxElapsedTime, valid.InitialDelay, valid.MaxDelay},
		{-1, valid.MaxElapsedTime, valid.InitialDelay, valid.MaxDelay},
		{1, 0, valid.InitialDelay, valid.MaxDelay},
		{1, -time.Second, valid.InitialDelay, valid.MaxDelay},
		{1, valid.MaxElapsedTime, 0, valid.MaxDelay},
		{1, valid.MaxElapsedTime, -time.Second, valid.MaxDelay},
		{1, valid.MaxElapsedTime, valid.InitialDelay, 0},
		{1, valid.MaxElapsedTime, valid.InitialDelay, valid.InitialDelay / 2},
	}
	for _, policy := range invalid {
		if _, err := NewClient(WithAPIKey(apiKey), WithRetryPolicy(policy)); err == nil {
			t.Errorf("accepted invalid policy %+v", policy)
		}
	}
	valid.MaxAttempts = 1
	if _, err := NewClient(WithAPIKey(apiKey), WithRetryPolicy(valid)); err != nil {
		t.Fatalf("one-attempt policy rejected: %v", err)
	}
}

func TestRetryDefaultOff(t *testing.T) {
	for _, body := range []string{`{"error":{"code":503,"message":"unavailable"}}`, `{"status":"UNKNOWN_ERROR","error_message":"try later"}`} {
		status := http.StatusServiceUnavailable
		if strings.Contains(body, "UNKNOWN_ERROR") {
			status = http.StatusOK
		}
		calls := 0
		b := &retryTestBody{Reader: strings.NewReader(body)}
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return retryTestResponse(status, b), nil
		}))
		err := retryTestGeocode(c, context.Background())
		if err == nil || calls != 1 || atomic.LoadInt32(&b.closes) != 1 {
			t.Fatalf("error=%v requests=%d closes=%d; want one failed, closed attempt", err, calls, b.closes)
		}
		if status == http.StatusOK {
			if apiErr, ok := err.(*APIError); !ok || apiErr.Status != "UNKNOWN_ERROR" {
				t.Fatalf("error=%T: %v; want UNKNOWN_ERROR", err, err)
			}
		} else if httpErr, ok := err.(*HTTPError); !ok || httpErr.StatusCode != status {
			t.Fatalf("error=%T: %v; want HTTP %d", err, err, status)
		}
	}
}

func TestRetryHTTPClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		retry  bool
	}{
		{"opaque 500", 500, "upstream failed", true},
		{"opaque 502", 502, "", true},
		{"opaque 503", 503, "{", true},
		{"opaque 504", 504, "", true},
		{"rate limit", 429, `{"error":{"code":429,"errors":[{"reason":"rateLimitExceeded"}]}}`, true},
		{"user rate limit", 429, `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`, true},
		{"structured rate limit", 503, `{"error":{"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"RATE_LIMIT_EXCEEDED"}]}}`, true},
		{"structured user rate limit", 429, `{"error":{"details":[{"reason":"USER_RATE_LIMIT_EXCEEDED"}]}}`, true},
		{"independent service code", 429, `{"error":{"code":8,"status":"RESOURCE_EXHAUSTED","errors":[{"reason":"rateLimitExceeded"}]}}`, true},
		{"opaque quota", 429, "", false},
		{"ambiguous quota", 503, `{"error":{"status":"RESOURCE_EXHAUSTED"}}`, false},
		{"ambiguous legacy quota", 503, `{"status":"OVER_QUERY_LIMIT"}`, false},
		{"daily quota", 503, `{"error":{"errors":[{"reason":"dailyLimitExceeded"}]}}`, false},
		{"daily status overrides rate reason", 503, `{"error":{"status":"OVER_DAILY_LIMIT","errors":[{"reason":"rateLimitExceeded"}]}}`, false},
		{"authentication", 503, `{"error":{"status":"UNAUTHENTICATED"}}`, false},
		{"permission", 503, `{"error":{"status":"PERMISSION_DENIED"}}`, false},
		{"invalid argument", 503, `{"error":{"status":"INVALID_ARGUMENT"}}`, false},
		{"invalid key overrides rate reason", 503, `{"error":{"errors":[{"reason":"rateLimitExceeded"},{"reason":"keyInvalid"}]}}`, false},
		{"unknown reason", 503, `{"error":{"errors":[{"reason":"futureReason"}]}}`, false},
		{"permanent status", 400, `{"error":{"errors":[{"reason":"rateLimitExceeded"}]}}`, false},
		{"not selected 501", 501, "", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			first := &retryTestBody{Reader: strings.NewReader(test.body)}
			last := &retryTestBody{Reader: strings.NewReader(`{"status":"OK","results":[{"place_id":"fresh"}]}`)}
			c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/maps/api/geocode/json" || r.URL.Query().Get("address") != "Sydney" || r.URL.Query().Get("key") != apiKey {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if calls == 1 {
					return retryTestResponse(test.status, first), nil
				}
				return retryTestResponse(200, last), nil
			}), WithRetryPolicy(retryTestPolicy()))
			result, err := c.Geocode(context.Background(), &GeocodingRequest{Address: "Sydney"})
			if test.retry {
				if err != nil || calls != 2 || len(result.Results) != 1 || result.Results[0].PlaceID != "fresh" || last.closes != 1 {
					t.Fatalf("result=%+v error=%v requests=%d success closes=%d", result, err, calls, last.closes)
				}
			} else {
				httpErr, ok := err.(*HTTPError)
				if !ok || httpErr.StatusCode != test.status || calls != 1 || len(result.Results) != 0 {
					t.Fatalf("result=%+v error=%T: %v requests=%d; want final HTTP %d", result, err, err, calls, test.status)
				}
			}
			if first.closes != 1 {
				t.Fatalf("discarded/error body closed %d times", first.closes)
			}
		})
	}
}

func TestRetryAttemptExhaustionPreservesLastError(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		calls := 0
		var bodies []*retryTestBody
		policy := retryTestPolicy()
		option := WithRetryPolicy(policy)
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			status := 503
			body := fmt.Sprintf(`{"error":{"code":503,"status":"UNAVAILABLE","message":"failure %d"}}`, calls)
			if legacy {
				status = 200
				body = fmt.Sprintf(`{"status":"UNKNOWN_ERROR","error_message":"failure %d","results":[{"place_id":"discard"}]}`, calls)
			}
			b := &retryTestBody{Reader: strings.NewReader(body)}
			bodies = append(bodies, b)
			return retryTestResponse(status, b), nil
		}), option)
		result, err := c.Geocode(context.Background(), &GeocodingRequest{Address: "Sydney"})
		if calls != 3 || len(result.Results) != 0 {
			t.Fatalf("legacy=%v requests=%d result=%+v", legacy, calls, result)
		}
		if legacy {
			apiErr, ok := err.(*APIError)
			if !ok || apiErr.Status != "UNKNOWN_ERROR" || apiErr.Message != "failure 3" {
				t.Fatalf("error=%T: %v; want last API error", err, err)
			}
		} else {
			httpErr, ok := err.(*HTTPError)
			if !ok || httpErr.StatusCode != 503 || httpErr.Code != 503 || httpErr.Status != "UNAVAILABLE" || httpErr.Message != "failure 3" {
				t.Fatalf("error=%T: %v; want last HTTP error", err, err)
			}
		}
		for i, b := range bodies {
			if b.closes != 1 {
				t.Errorf("attempt %d body closed %d times", i+1, b.closes)
			}
		}
	}
}

func TestRetryHTTPClientErrorBodyTimeout(t *testing.T) {
	for _, binary := range []bool{false, true} {
		t.Run(fmt.Sprintf("binary=%v", binary), func(t *testing.T) {
			var calls int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, "{")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer server.Close()
			httpClient := &http.Client{Timeout: 25 * time.Millisecond}
			c, err := NewClient(WithAPIKey(apiKey), WithBaseURL(server.URL),
				WithHTTPClient(httpClient), WithRateLimit(0), WithRetryPolicy(retryTestPolicy()))
			if err != nil {
				t.Fatal(err)
			}
			if binary {
				_, err = c.PlacePhoto(context.Background(), &PlacePhotoRequest{PhotoReference: "fixture", MaxWidth: 1})
			} else {
				err = retryTestGeocode(c, context.Background())
			}
			timeout, ok := err.(net.Error)
			if !ok || !timeout.Timeout() || atomic.LoadInt32(&calls) != 1 {
				t.Fatalf("error=%T: %v requests=%d; want one body-read timeout without retries", err, err, calls)
			}
		})
	}
}

func TestRetryLegacyStatusesAndFreshResults(t *testing.T) {
	for _, status := range []string{"UNKNOWN_ERROR", "OVER_QUERY_LIMIT", "OVER_DAILY_LIMIT", "REQUEST_DENIED", "INVALID_REQUEST", "FUTURE_STATUS", "OK", "ZERO_RESULTS"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body := fmt.Sprintf(`{"status":%q,"error_message":"first","results":[{"place_id":"stale"}]}`, status)
				if calls > 1 {
					// Omit results entirely: a decode into the old response would retain stale data.
					body = `{"status":"ZERO_RESULTS"}`
				}
				return retryTestResponse(200, &retryTestBody{Reader: strings.NewReader(body)}), nil
			}), WithRetryPolicy(retryTestPolicy()))
			result, err := c.Geocode(context.Background(), &GeocodingRequest{Address: "Sydney"})
			switch status {
			case "UNKNOWN_ERROR":
				if err != nil || calls != 2 || len(result.Results) != 0 {
					t.Fatalf("stale result=%+v error=%v requests=%d", result, err, calls)
				}
			case "OK", "ZERO_RESULTS":
				if err != nil || calls != 1 || len(result.Results) != 1 || result.Results[0].PlaceID != "stale" {
					t.Fatalf("result=%+v error=%v requests=%d; success must not retry", result, err, calls)
				}
			default:
				apiErr, ok := err.(*APIError)
				if !ok || apiErr.Status != status || apiErr.Message != "first" || calls != 1 || len(result.Results) != 0 {
					t.Fatalf("result=%+v error=%T: %v requests=%d", result, err, err, calls)
				}
			}
		})
	}
}

func TestRetryDoesNotRetryTransportOrDecodeErrors(t *testing.T) {
	for _, transportError := range []bool{false, true} {
		calls := 0
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if transportError {
				return nil, io.ErrUnexpectedEOF
			}
			return retryTestResponse(200, &retryTestBody{Reader: strings.NewReader(`{`)}), nil
		}), WithRetryPolicy(retryTestPolicy()))
		if err := retryTestGeocode(c, context.Background()); err == nil || calls != 1 {
			t.Fatalf("transport error=%v requests=%d error=%v", transportError, calls, err)
		}
	}
}

func TestRetryGeolocationReplaysExactPOST(t *testing.T) {
	request := &GeolocationRequest{HomeMobileCountryCode: 310, RadioType: RadioTypeLTE, Carrier: "a&b", ConsiderIP: true, WiFiAccessPoints: []WiFiAccessPoint{{MACAddress: "00:11:22:33:44:55", SignalStrength: -42}}}
	original, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var payloads [][]byte
	var bodies []*retryTestBody
	c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/geolocation/v1/geolocate" || r.Header.Get("Content-Type") != "application/json" || r.URL.Query().Get("key") != apiKey {
			t.Errorf("unexpected POST: %s %s headers=%v", r.Method, r.URL, r.Header)
		}
		payload, readErr := retryTestReadAll(r.Body)
		r.Body.Close()
		if readErr != nil {
			t.Errorf("request read: %v", readErr)
		}
		payloads = append(payloads, payload)
		status, text := 503, `{"error":{"code":503,"message":"unavailable"}}`
		if len(payloads) == 3 {
			status, text = 200, `{"location":{"lat":1.5,"lng":2.5},"accuracy":7}`
		}
		body := &retryTestBody{Reader: strings.NewReader(text)}
		bodies = append(bodies, body)
		return retryTestResponse(status, body), nil
	}), WithRetryPolicy(retryTestPolicy()))
	result, err := c.Geolocate(context.Background(), request)
	if err != nil || result == nil || result.Location.Lat != 1.5 || result.Location.Lng != 2.5 || result.Accuracy != 7 || len(payloads) != 3 {
		t.Fatalf("result=%+v error=%v POSTs=%d", result, err, len(payloads))
	}
	for i, payload := range payloads {
		if !bytes.Equal(payload, original) || bodies[i].closes != 1 {
			t.Errorf("attempt %d payload=%s closes=%d; want %s and one close", i+1, payload, bodies[i].closes, original)
		}
	}
	after, err := json.Marshal(request)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatalf("caller request changed: %s, %v", after, err)
	}
}

// Blocking transports/readers observe the actual operation context; no server or
// sleep is needed to coordinate cancellation with an in-flight request/read.
type retryTestWaitingReader struct {
	ctx     context.Context
	entered chan struct{}
	once    sync.Once
}

func (r *retryTestWaitingReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

func TestRetryCancellationStopsFurtherRequests(t *testing.T) {
	for _, stage := range []string{"before request", "request", "body", "error body", "backoff"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			var calls int32
			var body *retryTestBody
			policy := retryTestPolicy()
			// Equal jitter cannot finish this backoff before cancellation is sent.
			policy.InitialDelay, policy.MaxDelay = 2*time.Second, 2*time.Second
			policy.MaxElapsedTime = 5 * time.Second
			c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				switch stage {
				case "request":
					close(entered)
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "body", "error body":
					body = &retryTestBody{Reader: &retryTestWaitingReader{ctx: r.Context(), entered: entered}}
					status := 200
					if stage == "error body" {
						status = 503
					}
					return retryTestResponse(status, body), nil
				default:
					body = &retryTestBody{Reader: strings.NewReader("unavailable"), onClose: func() { close(entered) }}
					return retryTestResponse(503, body), nil
				}
			}), WithRetryPolicy(policy))
			if stage == "before request" {
				cancel()
			}
			done := make(chan error, 1)
			go func() { done <- retryTestGeocode(c, ctx) }()
			if stage != "before request" {
				retryTestWait(t, entered)
				if stage == "backoff" {
					// Let the response close before cancelling the much longer wait.
					timer := time.AfterFunc(25*time.Millisecond, cancel)
					defer timer.Stop()
				} else {
					cancel()
				}
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(250 * time.Millisecond):
				t.Fatal("cancellation did not promptly interrupt the retry operation")
			}
			wantCalls := int32(1)
			if stage == "before request" {
				wantCalls = 0
			}
			if retryTestContextError(err) != context.Canceled || atomic.LoadInt32(&calls) != wantCalls {
				t.Fatalf("error=%T: %v requests=%d; want canceled and %d requests", err, err, calls, wantCalls)
			}
			if body != nil && atomic.LoadInt32(&body.closes) != 1 {
				t.Fatalf("response body closed %d times", body.closes)
			}
		})
	}
}

func TestRetryEarlierTimeoutWins(t *testing.T) {
	for _, boundary := range []string{"budget", "caller", "HTTP client", "JSON body"} {
		t.Run(boundary, func(t *testing.T) {
			policy := retryTestPolicy()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if boundary == "budget" || boundary == "JSON body" {
				policy.MaxElapsedTime = 25 * time.Millisecond
			}
			if boundary == "caller" {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
				defer cancel()
			}
			calls := int32(0)
			var body *retryTestBody
			tr := retryTestTransport(func(r *http.Request) (*http.Response, error) {
				atomic.AddInt32(&calls, 1)
				if boundary != "HTTP client" {
					deadline, ok := r.Context().Deadline()
					if !ok || deadline.After(time.Now().Add(200*time.Millisecond)) {
						t.Errorf("earlier %s timeout did not bound request context: deadline=%v present=%v", boundary, deadline, ok)
					}
				}
				if boundary == "JSON body" {
					body = &retryTestBody{Reader: &retryTestWaitingReader{ctx: r.Context(), entered: make(chan struct{})}}
					return retryTestResponse(200, body), nil
				}
				select {
				case <-r.Context().Done():
					return nil, r.Context().Err()
				case <-r.Cancel:
					// Go 1.7 signals Client.Timeout through Request.Cancel.
					return nil, fmt.Errorf("HTTP client cancelled the request")
				}
			})
			httpClient := &http.Client{Transport: tr}
			if boundary == "HTTP client" {
				httpClient.Timeout = 25 * time.Millisecond
			}
			c := retryTestClient(t, tr, WithHTTPClient(httpClient), WithRetryPolicy(policy))
			done := make(chan error, 1)
			go func() { done <- retryTestGeocode(c, ctx) }()
			err := retryTestResult(t, done)
			if err == nil || atomic.LoadInt32(&calls) != 1 {
				t.Fatalf("error=%v requests=%d; want one timed-out request", err, calls)
			}
			if boundary == "HTTP client" {
				timeout, ok := err.(net.Error)
				if !ok || !timeout.Timeout() || httpClient.Timeout != 25*time.Millisecond {
					t.Fatalf("custom client timeout lost: error=%T: %v timeout=%v", err, err, httpClient.Timeout)
				}
			} else if retryTestContextError(err) != context.DeadlineExceeded {
				t.Fatalf("error=%T: %v; want deadline exceeded", err, err)
			}
			if boundary == "budget" && ctx.Err() != nil {
				t.Fatalf("retry budget canceled the caller context: %v", ctx.Err())
			}
			if body != nil && body.closes != 1 {
				t.Fatalf("timed-out body closed %d times", body.closes)
			}
		})
	}
}

func TestRetryBudgetAndRateLimitBoundBackoff(t *testing.T) {
	for _, rateLimited := range []bool{false, true} {
		calls := 0
		policy := retryTestPolicy()
		policy.MaxElapsedTime = 25 * time.Millisecond
		if !rateLimited {
			policy.InitialDelay, policy.MaxDelay = time.Second, time.Second
		}
		options := []ClientOption{WithRetryPolicy(policy)}
		if rateLimited {
			options = append(options, WithRateLimit(1))
		}
		body := &retryTestBody{Reader: strings.NewReader("unavailable")}
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			return retryTestResponse(503, body), nil
		}), options...)
		done := make(chan error, 1)
		go func() { done <- retryTestGeocode(c, context.Background()) }()
		var err error
		select {
		case err = <-done:
		case <-time.After(250 * time.Millisecond):
			t.Fatal("elapsed budget did not interrupt backoff before its 500 ms minimum")
		}
		if err == nil || calls != 1 || body.closes != 1 {
			t.Fatalf("rate limited=%v error=%v requests=%d closes=%d", rateLimited, err, calls, body.closes)
		}
		if !rateLimited && retryTestContextError(err) != context.DeadlineExceeded {
			t.Fatalf("backoff error=%T: %v; want budget deadline", err, err)
		}
	}
}

type retryTestBrokenReader struct{}

func (retryTestBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRetryPhotoStreamOwnershipAndReadFailure(t *testing.T) {
	for _, broken := range []bool{false, true} {
		calls := 0
		var streamCtx context.Context
		first := &retryTestBody{Reader: strings.NewReader("unavailable")}
		var reader io.Reader = strings.NewReader("photo bytes")
		if broken {
			reader = retryTestBrokenReader{}
		}
		imageBody := &retryTestBody{Reader: reader}
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return retryTestResponse(503, first), nil
			}
			streamCtx = r.Context()
			resp := retryTestResponse(200, imageBody)
			resp.Header.Set("Content-Type", "image/jpeg")
			return resp, nil
		}), WithRetryPolicy(retryTestPolicy()))
		photo, err := c.PlacePhoto(context.Background(), &PlacePhotoRequest{PhotoReference: "reference", MaxWidth: 100})
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if atomic.LoadInt32(&imageBody.closes) == 0 {
				photo.Data.Close()
			}
		}()
		if calls != 2 || first.closes != 1 || imageBody.reads != 0 || imageBody.closes != 0 || photo.ContentType != "image/jpeg" || streamCtx.Err() != nil {
			t.Fatalf("requests=%d discarded closes=%d stream reads/closes=%d/%d type=%s context=%v", calls, first.closes, imageBody.reads, imageBody.closes, photo.ContentType, streamCtx.Err())
		}
		data, readErr := retryTestReadAll(photo.Data)
		if broken {
			if readErr != io.ErrUnexpectedEOF {
				t.Fatalf("read error=%v; want original stream failure", readErr)
			}
		} else if readErr != nil || string(data) != "photo bytes" {
			t.Fatalf("data=%q error=%v", data, readErr)
		}
		if calls != 2 || imageBody.closes != 0 {
			t.Fatalf("stream read retried or closed caller body: requests=%d closes=%d", calls, imageBody.closes)
		}
		photo.Data.Close()
		if imageBody.closes != 1 {
			t.Fatalf("caller close reached body %d times", imageBody.closes)
		}
		select {
		case <-streamCtx.Done():
		default:
			t.Fatal("closing stream did not release its retry context")
		}
	}
}

func TestRetryPhotoBudgetAndCancellationBoundHandedOffReads(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		policy := retryTestPolicy()
		if !canceled {
			policy.MaxElapsedTime = 25 * time.Millisecond
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		entered := make(chan struct{})
		calls := int32(0)
		var body *retryTestBody
		c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			body = &retryTestBody{Reader: &retryTestWaitingReader{ctx: r.Context(), entered: entered}}
			return retryTestResponse(200, body), nil
		}), WithRetryPolicy(policy))
		photo, err := c.PlacePhoto(ctx, &PlacePhotoRequest{PhotoReference: "reference", MaxWidth: 100})
		if err != nil {
			t.Fatal(err)
		}
		defer photo.Data.Close()
		done := make(chan error, 1)
		go func() { _, readErr := retryTestReadAll(photo.Data); done <- readErr }()
		retryTestWait(t, entered)
		want := context.DeadlineExceeded
		if canceled {
			cancel()
			want = context.Canceled
		}
		if readErr := retryTestResult(t, done); readErr != want || atomic.LoadInt32(&calls) != 1 || body.closes != 0 {
			t.Fatalf("read error=%v requests=%d closes=%d; stream remains caller-owned without retry", readErr, calls, body.closes)
		}
	}
}

func TestRetryPhotoExhaustionClosesEveryErrorBody(t *testing.T) {
	var bodies []*retryTestBody
	c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
		body := &retryTestBody{Reader: strings.NewReader(fmt.Sprintf(`{"error":{"code":503,"status":"UNAVAILABLE","message":"photo failure %d"}}`, len(bodies)+1))}
		bodies = append(bodies, body)
		return retryTestResponse(503, body), nil
	}), WithRetryPolicy(retryTestPolicy()))
	photo, err := c.PlacePhoto(context.Background(), &PlacePhotoRequest{PhotoReference: "reference", MaxWidth: 100})
	httpErr, ok := err.(*HTTPError)
	if !ok || httpErr.StatusCode != 503 || httpErr.Code != 503 || httpErr.Status != "UNAVAILABLE" || httpErr.Message != "photo failure 3" || photo.Data != nil || len(bodies) != 3 {
		t.Fatalf("photo=%+v error=%T: %v requests=%d", photo, err, err, len(bodies))
	}
	for i, body := range bodies {
		if body.closes != 1 {
			t.Errorf("photo attempt %d closed %d times", i+1, body.closes)
		}
	}
}

func TestRetryStaticMapDoesNotRetryImageReadFailure(t *testing.T) {
	calls := 0
	body := &retryTestBody{Reader: retryTestBrokenReader{}}
	c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return retryTestResponse(200, body), nil
	}), WithRetryPolicy(retryTestPolicy()))
	result, err := c.StaticMap(context.Background(), &StaticMapRequest{Center: "Sydney", Zoom: 10, Size: "100x100"})
	if result != nil || err == nil || calls != 1 || body.closes != 1 {
		t.Fatalf("image=%v error=%v requests=%d closes=%d", result, err, calls, body.closes)
	}
}

func TestRetryConcurrentClientOperations(t *testing.T) {
	const workers = 12
	var mu sync.Mutex
	attempts := make(map[string]int)
	var bodies []*retryTestBody
	policy := retryTestPolicy()
	policy.InitialDelay, policy.MaxDelay = time.Nanosecond, time.Nanosecond
	c := retryTestClient(t, retryTestTransport(func(r *http.Request) (*http.Response, error) {
		address := r.URL.Query().Get("address")
		mu.Lock()
		attempts[address]++
		attempt := attempts[address]
		status, text := 503, "unavailable"
		if attempt == 2 {
			status, text = 200, fmt.Sprintf(`{"status":"OK","results":[{"place_id":%q}]}`, address)
		}
		body := &retryTestBody{Reader: strings.NewReader(text)}
		bodies = append(bodies, body)
		mu.Unlock()
		return retryTestResponse(status, body), nil
	}), WithRetryPolicy(policy))
	start := make(chan struct{})
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			<-start
			address := fmt.Sprintf("address-%d", i)
			result, err := c.Geocode(context.Background(), &GeocodingRequest{Address: address})
			if err == nil && (len(result.Results) != 1 || result.Results[0].PlaceID != address) {
				err = fmt.Errorf("operation %s got %+v", address, result)
			}
			results <- err
		}(i)
	}
	close(start)
	for i := 0; i < workers; i++ {
		if err := retryTestResult(t, results); err != nil {
			t.Error(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	want := make(map[string]int)
	for i := 0; i < workers; i++ {
		want[fmt.Sprintf("address-%d", i)] = 2
	}
	if !reflect.DeepEqual(attempts, want) || len(bodies) != 2*workers {
		t.Fatalf("attempts=%v body count=%d", attempts, len(bodies))
	}
	for _, body := range bodies {
		if atomic.LoadInt32(&body.closes) != 1 {
			t.Errorf("concurrent operation body closed %d times", body.closes)
		}
	}
}
