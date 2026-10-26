package ingestion

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRemoteResponseBytes = 2 << 20
const maxSafeInteger int64 = 9007199254740991
const maxUnixTimestamp int64 = 253402300799

type remoteError struct{ code string }

func (e *remoteError) Error() string { return e.code }

func remoteErrorCode(err error) string {
	var problem *remoteError
	if errors.As(err, &problem) {
		return problem.code
	}
	return "NETWORK_ERROR"
}

func isRemoteFailure(err error) bool {
	var problem *remoteError
	return errors.As(err, &problem)
}

type requestReport struct {
	Attempts    int
	NextRetryAt *time.Time
}

type requestController struct {
	db             *sql.DB
	runID          string
	client         *http.Client
	source         RemoteSource
	budget         *BudgetPolicy
	now            func() time.Time
	deadline       time.Time
	wait           func(context.Context, time.Duration) error
	jitter         func(time.Duration) time.Duration
	requestTimeout time.Duration
}

type responseMetadata struct {
	status int
	header http.Header
}

func applyRequestReport(result *ExecutionResult, report requestReport) {
	result.RequestsMade += report.Attempts
	if report.NextRetryAt != nil && (result.NextRetryAt == nil || report.NextRetryAt.After(*result.NextRetryAt)) {
		retryAt := *report.NextRetryAt
		result.NextRetryAt = &retryAt
	}
}

func (c requestController) JSON(ctx context.Context, request *http.Request, class RequestClass, destination any) (requestReport, error) {
	c = c.withDefaults()
	var report requestReport
	for attempt := 1; attempt <= 3; attempt++ {
		now := c.now().UTC()
		if !c.deadline.IsZero() && !now.Before(c.deadline) {
			return report, &remoteError{code: "TASK_DEADLINE_EXCEEDED"}
		}
		attemptRequest, err := cloneRequestForAttempt(request)
		if err != nil {
			return report, err
		}
		if c.budget != nil {
			_, err = ClaimRequestBudget(ctx, c.db, c.runID, c.source, class, *c.budget, now)
			if err != nil {
				var limited *RateLimitError
				if errors.As(err, &limited) {
					retry := limited.RetryAt
					report.NextRetryAt = &retry
					return report, &remoteError{code: "RATE_LIMITED"}
				}
				return report, err
			}
		}
		report.Attempts++
		metadata, err := requestJSONAttempt(ctx, c.client, attemptRequest, destination, c.requestTimeout)
		if (c.source == GitHub || c.source == ProductHunt) && c.db != nil {
			if recordErr := recordProviderBudgetHeaders(ctx, c.db, c.source, class, metadata.header, now); recordErr != nil {
				return report, recordErr
			}
		}
		if err == nil {
			return report, nil
		}
		if isGitHubRateLimitForbidden(c.source, metadata) {
			err = &remoteError{code: "RATE_LIMITED"}
		}
		if ctx.Err() != nil || !isRetryableRemoteError(c.source, metadata, err) {
			return report, err
		}
		retryAt := retryTime(c.source, metadata.header, now, attempt, c.jitter)
		if attempt == 3 || (!c.deadline.IsZero() && !retryAt.Before(c.deadline)) {
			report.NextRetryAt = &retryAt
			return report, err
		}
		if waitErr := c.wait(ctx, retryAt.Sub(now)); waitErr != nil {
			report.NextRetryAt = &retryAt
			return report, &remoteError{code: "NETWORK_ERROR"}
		}
	}
	return report, &remoteError{code: "NETWORK_ERROR"}
}

func cloneRequestForAttempt(request *http.Request) (*http.Request, error) {
	cloned := request.Clone(request.Context())
	if request.Body == nil {
		return cloned, nil
	}
	if request.GetBody == nil {
		return nil, errors.New("source request body is not replayable")
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, errors.New("source request body replay failed")
	}
	cloned.Body = body
	return cloned, nil
}

func (c requestController) withDefaults() requestController {
	if c.client == nil {
		c.client = newSourceHTTPClient(c.source)
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.wait == nil {
		c.wait = waitWithTimer
	}
	if c.jitter == nil {
		c.jitter = randomJitter
	}
	if c.requestTimeout <= 0 {
		c.requestTimeout = 15 * time.Second
	}
	return c
}

func requestJSONAttempt(ctx context.Context, client *http.Client, request *http.Request, destination any, timeout time.Duration) (responseMetadata, error) {
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := client.Do(request.Clone(requestContext))
	if err != nil {
		return responseMetadata{}, &remoteError{code: "NETWORK_ERROR"}
	}
	defer response.Body.Close()
	metadata := responseMetadata{status: response.StatusCode, header: response.Header.Clone()}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return metadata, &remoteError{code: remoteStatusCode(response.StatusCode)}
	}
	reader := io.Reader(response.Body)
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !response.Uncompressed {
		if !strings.EqualFold(encoding, "gzip") {
			return metadata, &remoteError{code: "MALFORMED_RESPONSE"}
		}
		compressed, gzipErr := gzip.NewReader(response.Body)
		if gzipErr != nil {
			return metadata, &remoteError{code: "MALFORMED_RESPONSE"}
		}
		defer compressed.Close()
		reader = compressed
	}
	limited := io.LimitReader(reader, maxRemoteResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return metadata, &remoteError{code: "NETWORK_ERROR"}
	}
	if len(body) > maxRemoteResponseBytes {
		return metadata, &remoteError{code: "RESPONSE_TOO_LARGE"}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err = decoder.Decode(destination); err != nil {
		return metadata, &remoteError{code: "MALFORMED_RESPONSE"}
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return metadata, &remoteError{code: "MALFORMED_RESPONSE"}
	}
	return metadata, nil
}

func isRetryableRemoteError(source RemoteSource, metadata responseMetadata, err error) bool {
	code := remoteErrorCode(err)
	if code == "NETWORK_ERROR" || metadata.status == http.StatusTooManyRequests || metadata.status >= 500 {
		return true
	}
	return isGitHubRateLimitForbidden(source, metadata)
}

func isGitHubRateLimitForbidden(source RemoteSource, metadata responseMetadata) bool {
	return source == GitHub && metadata.status == http.StatusForbidden &&
		(strings.TrimSpace(metadata.header.Get("X-RateLimit-Remaining")) == "0" || strings.TrimSpace(metadata.header.Get("Retry-After")) != "")
}

func retryTime(source RemoteSource, header http.Header, now time.Time, attempt int, jitter func(time.Duration) time.Duration) time.Time {
	base := time.Second << (attempt - 1)
	if base > 30*time.Second {
		base = 30 * time.Second
	}
	retryAt := now.Add(base + jitter(base))
	if headerAt, ok := retryAfterTime(header.Get("Retry-After"), now); ok && headerAt.After(retryAt) {
		retryAt = headerAt
	}
	if resetAt, ok := providerResetTime(source, rateLimitHeader(header, source, "Reset"), now); ok && resetAt.After(retryAt) {
		retryAt = resetAt
	}
	return retryAt
}

func retryAfterTime(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second), true
	}
	parsed, err := http.ParseTime(value)
	return parsed, err == nil && parsed.After(now)
}

func rateLimitResetTime(value string) (time.Time, bool) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds <= 0 || seconds > maxUnixTimestamp {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

func providerResetTime(source RemoteSource, value string, now time.Time) (time.Time, bool) {
	if source != ProductHunt {
		return rateLimitResetTime(value)
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return now.Add(time.Duration(seconds) * time.Second), true
}

func recordProviderBudgetHeaders(ctx context.Context, db *sql.DB, source RemoteSource, class RequestClass, header http.Header, observedAt time.Time) error {
	resource := providerResource(source, class)
	if reported := strings.ToLower(strings.TrimSpace(rateLimitHeader(header, source, "Resource"))); source == GitHub && validProviderResource(reported) {
		resource = reported
	}
	remaining, err := strconv.Atoi(strings.TrimSpace(rateLimitHeader(header, source, "Remaining")))
	maxRemaining := 5000
	if source == ProductHunt {
		maxRemaining = 1_000_000
	}
	if err != nil || remaining < 0 || remaining > maxRemaining {
		return nil
	}
	resetAt, ok := providerResetTime(source, rateLimitHeader(header, source, "Reset"), observedAt)
	if !ok || !resetAt.After(observedAt) {
		return nil
	}
	return RecordProviderBudget(ctx, db, source, resource, remaining, resetAt, observedAt)
}

func rateLimitHeader(header http.Header, source RemoteSource, field string) string {
	prefix := "X-RateLimit-"
	if source == ProductHunt {
		prefix = "X-Rate-Limit-"
	}
	return header.Get(prefix + field)
}

func waitWithTimer(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func randomJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0
	}
	return time.Duration(binary.LittleEndian.Uint64(buffer[:]) % uint64(base))
}

func remoteStatusCode(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "AUTH_FAILED"
	case status == http.StatusTooManyRequests:
		return "RATE_LIMITED"
	case status == http.StatusForbidden:
		return "FORBIDDEN"
	case status == http.StatusNotFound:
		return "NOT_FOUND"
	case status == http.StatusUnprocessableEntity:
		return "UPSTREAM_VALIDATION_FAILED"
	case status >= 500:
		return "UPSTREAM_UNAVAILABLE"
	default:
		return fmt.Sprintf("UPSTREAM_HTTP_%d", status)
	}
}

func appendRunError(result *ExecutionResult, code, message string, itemID *string, failure bool) {
	item := RunError{Code: code, Message: message, ItemID: itemID}
	if failure {
		if len(result.Errors) < 50 {
			result.Errors = append(result.Errors, item)
		}
		return
	}
	if len(result.Warnings) < 50 {
		result.Warnings = append(result.Warnings, item)
	}
}
