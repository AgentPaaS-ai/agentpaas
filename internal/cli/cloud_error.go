package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

const (
	cloudExitOther    = 1
	cloudExitQuota    = 2
	cloudExitAuth     = 3
	cloudExitNotFound = 4
	cloudExitCapacity = 5
	cloudExitConflict = 6
)

// CloudErrorJSON is the stable machine-readable error envelope for cloud
// commands. Cloud errors are written as one object to stdout in --json mode.
type CloudErrorJSON struct {
	Error         string `json:"error"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	RetryAfterSec int    `json:"retry_after_sec"`
}

// CloudCommandError preserves the API error while carrying the semantic exit
// code selected for a cloud command.
type CloudCommandError struct {
	cause    error
	payload  CloudErrorJSON
	exitCode int
	rendered bool
}

func (e *CloudCommandError) Error() string { return e.cause.Error() }

func (e *CloudCommandError) Unwrap() error { return e.cause }

// IsCloudErrorRendered reports whether the cloud error was already emitted as
// JSON by a Cobra command. The process entry point uses this to avoid a second
// error line on stderr.
func IsCloudErrorRendered(err error) bool {
	var cloudErr *CloudCommandError
	return errors.As(err, &cloudErr) && cloudErr.rendered
}

// CloudExitCode returns the semantic process exit code for a cloud error.
func CloudExitCode(err error) int {
	if err == nil {
		return 0
	}
	var cloudErr *CloudCommandError
	if errors.As(err, &cloudErr) {
		return cloudErr.exitCode
	}
	return classifyCloudError(err).exitCode
}

type classifiedCloudError struct {
	payload  CloudErrorJSON
	exitCode int
}

func wrapCloudCommandErrors(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		if child.RunE != nil {
			original := child.RunE
			child.RunE = func(runCmd *cobra.Command, args []string) error {
				err := original(runCmd, args)
				if err == nil {
					return nil
				}
				return renderCloudCommandError(runCmd, err)
			}
		}
		wrapCloudCommandErrors(child)
	}
}

func renderCloudCommandError(cmd *cobra.Command, err error) error {
	classified := classifyCloudError(err)
	cloudErr := &CloudCommandError{
		cause:    err,
		payload:  classified.payload,
		exitCode: classified.exitCode,
	}
	if !jsonOutput(cmd) {
		return cloudErr
	}

	data, marshalErr := json.Marshal(classified.payload)
	if marshalErr != nil {
		return &CloudCommandError{
			cause:    fmt.Errorf("cloud error: encode JSON error: %w", marshalErr),
			exitCode: cloudExitOther,
		}
	}
	if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), string(data)); writeErr != nil {
		return &CloudCommandError{
			cause:    fmt.Errorf("cloud error: write JSON error: %w", writeErr),
			exitCode: cloudExitOther,
		}
	}
	cloudErr.rendered = true
	return cloudErr
}

func classifyCloudError(err error) classifiedCloudError {
	payload := CloudErrorJSON{
		Error:         "cloud_error",
		Reason:        "other",
		Message:       err.Error(),
		RetryAfterSec: 0,
	}

	var statusErr *cloudclient.HTTPStatusError
	if errors.As(err, &statusErr) {
		payload.Error = statusErr.ErrorCode
		payload.Reason = statusErr.Reason
		payload.Message = statusErr.Message
		if statusErr.APIMessage != "" {
			payload.Message = statusErr.APIMessage
		}
		payload.RetryAfterSec = statusErr.RetryAfterSec
		if payload.Error == "" {
			payload.Error = statusErrorCode(statusErr.StatusCode)
		}
		if payload.Reason == "" {
			payload.Reason = payload.Error
		}
		return classifiedCloudError{payload: payload, exitCode: cloudExitCodeFor(payload.Error, payload.Reason)}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "not logged in"):
		payload.Error = "not logged in"
		payload.Reason = "unauthorized"
		payload.Message = "No cloud token found"
	case strings.Contains(lower, "not authenticated"), strings.Contains(lower, "unauthorized"):
		payload.Error = "unauthorized"
		payload.Reason = "unauthorized"
	case strings.Contains(lower, "already_running"), strings.Contains(lower, "already running"):
		payload.Error = "conflict"
		payload.Reason = "already_running"
	case strings.Contains(lower, "no_slot_capacity"):
		payload.Error = "capacity"
		payload.Reason = "no_slot_capacity"
	case strings.Contains(lower, "quota_exceeded"), strings.Contains(lower, "trial_expired"):
		payload.Error = "quota_exceeded"
		payload.Reason = firstCloudReason(lower, "quota_exceeded", "trial_expired")
	case strings.Contains(lower, "not_found"):
		payload.Error = "not_found"
		payload.Reason = "not_found"
	case strings.Contains(lower, "container_start_failed"):
		payload.Error = "container_start_failed"
		payload.Reason = "container_start_failed"
	}
	return classifiedCloudError{payload: payload, exitCode: cloudExitCodeFor(payload.Error, payload.Reason)}
}

func statusErrorCode(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "unauthorized"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusTooManyRequests:
		return "quota_exceeded"
	case http.StatusConflict:
		return "conflict"
	default:
		return "cloud_error"
	}
}

func firstCloudReason(message, first, second string) string {
	if strings.Contains(message, second) {
		return second
	}
	return first
}

func cloudExitCodeFor(code, reason string) int {
	code = strings.ToLower(code)
	reason = strings.ToLower(reason)
	switch {
	case code == "quota_exceeded" || code == "quota" || reason == "quota_exceeded" || reason == "trial_expired" || code == "trial_expired":
		return cloudExitQuota
	case code == "unauthorized" || code == "auth" || reason == "unauthorized":
		return cloudExitAuth
	case code == "not_found" || reason == "not_found":
		return cloudExitNotFound
	case code == "capacity" || reason == "no_slot_capacity":
		return cloudExitCapacity
	case code == "conflict" || reason == "already_running":
		return cloudExitConflict
	default:
		return cloudExitOther
	}
}
