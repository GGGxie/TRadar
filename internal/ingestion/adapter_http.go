package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxRemoteResponseBytes = 2 << 20
const maxSafeInteger int64 = 9007199254740991
const maxUnixTimestamp int64 = 253402300799

type remoteError struct {
	code string
}

func (e *remoteError) Error() string { return e.code }

func remoteErrorCode(err error) string {
	var problem *remoteError
	if errors.As(err, &problem) {
		return problem.code
	}
	return "NETWORK_ERROR"
}

func requestJSON(ctx context.Context, client *http.Client, request *http.Request, destination any) error {
	requestContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := client.Do(request.WithContext(requestContext))
	if err != nil {
		return &remoteError{code: "NETWORK_ERROR"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &remoteError{code: remoteStatusCode(response.StatusCode)}
	}
	limited := io.LimitReader(response.Body, maxRemoteResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return &remoteError{code: "NETWORK_ERROR"}
	}
	if len(body) > maxRemoteResponseBytes {
		return &remoteError{code: "RESPONSE_TOO_LARGE"}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err = decoder.Decode(destination); err != nil {
		return &remoteError{code: "MALFORMED_RESPONSE"}
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return &remoteError{code: "MALFORMED_RESPONSE"}
	}
	return nil
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
