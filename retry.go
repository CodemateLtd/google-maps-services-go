// Copyright 2026 Google Inc. All Rights Reserved.
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
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"time"
)

// RetryPolicy bounds opt-in retries of transient service failures. The zero
// value is invalid; without WithRetryPolicy, requests are attempted only once.
// A Client copies the policy and is not affected by subsequent changes to it.
//
// Only legacy APIError status UNKNOWN_ERROR and selected HTTPError responses
// are retried. HTTP candidates are 429, 500, 502, 503 and 504. A permanent or
// unrecognized nonempty service status or reason vetoes a retry. Empty status,
// UNKNOWN_ERROR, INTERNAL, UNAVAILABLE and DEADLINE_EXCEEDED are transient-neutral,
// as are backendError, internalError, BACKEND_ERROR and INTERNAL_ERROR reasons.
// HTTP 429, RESOURCE_EXHAUSTED and OVER_QUERY_LIMIT additionally require an
// explicit rateLimitExceeded, userRateLimitExceeded, RATE_LIMIT_EXCEEDED or
// USER_RATE_LIMIT_EXCEEDED reason in errors or details. Numeric service codes
// are retained as metadata, not interpreted as HTTP status codes. Opaque 5xx
// responses may retry, but ambiguous quota responses and daily limits do not.
// Network errors and failures reading an already delivered binary stream are
// never retried. No decisions are based on error-message text.
//
// Each attempt uses the client's rate limiter and HTTP client. Geolocation POST
// requests are marshaled once and replayed unchanged. Backoff uses equal jitter
// between half and all of an exponentially growing, capped delay.
type RetryPolicy struct {
	// MaxAttempts includes the initial attempt and must be at least one.
	MaxAttempts int
	// MaxElapsedTime must be positive and bounds the entire operation, including
	// rate limiting, requests, JSON decoding and backoff. An earlier caller
	// deadline or the custom HTTP client's timeout still applies. For successful
	// binary responses this budget also bounds body reads: the request context
	// remains live until the caller closes Data (or StaticMap finishes reading).
	// A binary response is never retried after it has been handed to its reader.
	MaxElapsedTime time.Duration
	// InitialDelay must be positive and is the first backoff's upper bound.
	InitialDelay time.Duration
	// MaxDelay caps exponential backoff and must be at least InitialDelay.
	MaxDelay time.Duration
}

// WithRetryPolicy enables bounded transient retries using a copy of policy.
// Invalid fields cause NewClient to return an error. Retries are disabled unless
// this option is supplied; MaxAttempts of one still enables the elapsed-time
// budget. This option does not modify the HTTP client, transport or its timeout.
func WithRetryPolicy(policy RetryPolicy) ClientOption {
	return func(client *Client) error {
		if policy.MaxAttempts < 1 {
			return errors.New("maps: RetryPolicy.MaxAttempts must be at least 1")
		}
		if policy.MaxElapsedTime <= 0 {
			return errors.New("maps: RetryPolicy.MaxElapsedTime must be positive")
		}
		if policy.InitialDelay <= 0 {
			return errors.New("maps: RetryPolicy.InitialDelay must be positive")
		}
		if policy.MaxDelay < policy.InitialDelay {
			return errors.New("maps: RetryPolicy.MaxDelay must be at least InitialDelay")
		}
		copy := policy
		client.retryPolicy = &copy
		return nil
	}
}

func (c *Client) retryJSON(ctx context.Context, config *apiConfig, apiReq interface{}, resp interface{}, post bool) error {
	_, err := c.retryRequest(ctx, config, apiReq, resp, post)
	return err
}

func (c *Client) retryBinary(ctx context.Context, config *apiConfig, apiReq apiRequest) (binaryResponse, error) {
	response, err := c.retryRequest(ctx, config, apiReq, nil, false)
	if err != nil {
		if httpErr, ok := err.(*HTTPError); ok && config == placesPhotoAPI && httpErr.StatusCode == http.StatusForbidden && httpErr.Message == "" {
			httpErr.Message = "request forbidden; check your API permissions and available quota"
		}
		return binaryResponse{}, err
	}
	return binaryResponse{response.StatusCode, response.Header.Get("Content-Type"), response.Body}, nil
}

// A nil resp selects binary delivery; otherwise resp is the JSON destination.
// Only successful binary delivery transfers the response body and cancel function.
func (c *Client) retryRequest(ctx context.Context, config *apiConfig, apiReq interface{}, resp interface{}, post bool) (response *http.Response, err error) {
	policy := c.retryPolicy
	ctx, cancel := context.WithTimeout(ctx, policy.MaxElapsedTime)
	bodyOwnedByCaller := false
	defer func() {
		if !bodyOwnedByCaller {
			cancel()
		}
	}()
	requestMetrics := c.metricReporter.NewRequest(config.path)
	defer func() {
		metro := ""
		if response != nil {
			metro = response.Header.Get("x-goog-maps-metro-area")
		}
		requestMetrics.EndRequest(ctx, err, response, metro)
	}()

	var body []byte
	if post {
		body, err = json.Marshal(apiReq)
		if err != nil {
			return nil, err
		}
	}
	delay := policy.InitialDelay
	for attempt := 1; ; attempt++ {
		if err = ctx.Err(); err != nil {
			return response, err
		}
		if post {
			response, err = c.postBytes(ctx, config, body)
		} else {
			response, err = c.get(ctx, config, apiReq.(apiRequest))
		}
		if err == nil {
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				reader := retryErrorReader{Reader: response.Body}
				err = decodeHTTPError(response.StatusCode, &reader)
				if reader.err != nil {
					// The HTTP decoder intentionally tolerates missing diagnostics,
					// but a body-read failure is not a retryable service response.
					err = reader.err
				}
			} else if resp != nil {
				if attempt > 1 {
					// JSON decoding merges into existing values. Reset everything so
					// an UNKNOWN_ERROR response cannot leak fields into a success.
					destination := reflect.ValueOf(resp).Elem()
					destination.Set(reflect.Zero(destination.Type()))
				}
				err = decodeJSONResponse(response, resp)
				if err == nil {
					if status, ok := resp.(interface{ StatusError() error }); ok {
						err = status.StatusError()
					}
				}
			}
			if ctx.Err() != nil {
				err = ctx.Err()
			}
			if resp != nil || err != nil {
				response.Body.Close()
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			if err == nil && resp == nil {
				response.Body.Close()
			}
			return response, ctxErr
		}
		if err == nil {
			if resp == nil {
				response.Body = &retryBody{ReadCloser: response.Body, cancel: cancel}
				bodyOwnedByCaller = true
			}
			return response, nil
		}
		if attempt >= policy.MaxAttempts || !retryableError(err) {
			return response, err
		}
		if err = waitRetry(ctx, delay); err != nil {
			return response, err
		}
		if delay > policy.MaxDelay-delay {
			delay = policy.MaxDelay
		} else {
			delay *= 2
		}
	}
}

type retryErrorReader struct {
	io.Reader
	err error
}

func (reader *retryErrorReader) Read(p []byte) (int, error) {
	n, err := reader.Reader.Read(p)
	if err != nil && err != io.EOF {
		reader.err = err
	}
	return n, err
}

type retryBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *retryBody) Close() error {
	body.cancel()
	return body.ReadCloser.Close()
}

func waitRetry(ctx context.Context, cap time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	half := cap - cap/2
	span := uint64(cap-half) + 1
	delay := half
	if span > 1 {
		minimum := -span % span
		// Rejection sampling avoids modulo bias without allocating a generator
		// or sharing mutable random state. crypto/rand is available on Go 1.7.
		var entropy [8]byte
		var sample uint64
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, err := rand.Read(entropy[:]); err != nil {
				return err
			}
			sample = binary.LittleEndian.Uint64(entropy[:])
			if sample >= minimum {
				break
			}
		}
		delay += time.Duration(sample % span)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func retryableError(err error) bool {
	switch failure := err.(type) {
	case *APIError:
		return failure.Status == "UNKNOWN_ERROR"
	case *HTTPError:
		if !retryableHTTPStatus(failure.StatusCode) {
			return false
		}
		quota := failure.StatusCode == http.StatusTooManyRequests
		switch failure.Status {
		case "", "UNKNOWN_ERROR", "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
		case "RESOURCE_EXHAUSTED", "OVER_QUERY_LIMIT":
			quota = true
		default:
			return false
		}
		rateLimited := false
		for _, records := range []json.RawMessage{failure.Errors, failure.Details} {
			if len(records) == 0 {
				continue
			}
			var reasons []struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(records, &reasons) != nil {
				return false
			}
			for _, record := range reasons {
				switch record.Reason {
				case "":
				case "rateLimitExceeded", "userRateLimitExceeded", "RATE_LIMIT_EXCEEDED", "USER_RATE_LIMIT_EXCEEDED":
					rateLimited = true
				case "backendError", "internalError", "BACKEND_ERROR", "INTERNAL_ERROR":
				default:
					return false
				}
			}
		}
		return !quota || rateLimited
	}
	return false
}

func retryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}
