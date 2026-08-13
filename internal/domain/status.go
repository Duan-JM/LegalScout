// Package domain contains the durable vocabulary used by LegalScout.
package domain

import (
	"fmt"
	"strings"
)

// ProjectStatus describes the lifecycle of one isolated client matter.
type ProjectStatus string

const (
	ProjectDraft       ProjectStatus = "draft"
	ProjectQueued      ProjectStatus = "queued"
	ProjectRunning     ProjectStatus = "running"
	ProjectNeedsReview ProjectStatus = "needs_review"
	ProjectCompleted   ProjectStatus = "completed"
	ProjectFailed      ProjectStatus = "failed"
	ProjectArchived    ProjectStatus = "archived"
)

// CheckStatus is deliberately more precise than a boolean result. In
// particular, operational errors must never be represented as NotFound.
type CheckStatus string

const (
	Pending        CheckStatus = "pending"
	Running        CheckStatus = "running"
	NotFound       CheckStatus = "not_found"
	Found          CheckStatus = "found"
	NeedsReview    CheckStatus = "needs_review"
	RetryableError CheckStatus = "retryable_error"
	FatalError     CheckStatus = "fatal_error"
)

func (s CheckStatus) IsConfirmed() bool { return s == Found || s == NotFound }
func (s CheckStatus) IsFailure() bool   { return s == RetryableError || s == FatalError }

func (s CheckStatus) Validate() error {
	switch s {
	case Pending, Running, NotFound, Found, NeedsReview, RetryableError, FatalError:
		return nil
	default:
		return fmt.Errorf("unknown check status %q", s)
	}
}

func ProjectStatusFor(checks []CheckStatus) ProjectStatus {
	if len(checks) == 0 {
		return ProjectDraft
	}
	var pending, running, review, failed bool
	var done int
	for _, status := range checks {
		switch status {
		case Pending:
			pending = true
		case Running:
			running = true
		case NeedsReview:
			review = true
		case RetryableError, FatalError:
			failed = true
		case Found, NotFound:
			done++
		}
	}
	switch {
	case running:
		return ProjectRunning
	case pending:
		return ProjectQueued
	case review:
		return ProjectNeedsReview
	case failed:
		return ProjectFailed
	case done == len(checks):
		return ProjectCompleted
	default:
		return ProjectDraft
	}
}

// ClassifyError deliberately has no route from an error to NotFound. Only a
// source's positive page rule may produce NotFound.
func ClassifyError(err error) CheckStatus {
	if err == nil {
		return Pending
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "selector") ||
		strings.Contains(message, "contract") ||
		strings.Contains(message, "unsupported") ||
		strings.Contains(message, "invalid") ||
		strings.Contains(message, "configuration") ||
		strings.Contains(message, "配置") ||
		strings.Contains(message, "结构") {
		return FatalError
	}
	return RetryableError
}

type Task struct {
	ID             int64
	SubjectID      int64
	Sequence       int
	Subject        string
	SourceID       string
	Status         CheckStatus
	Attempts       int
	LeaseUntil     int64
	LastError      string
	ScreenshotPath string
	ReplacesID     *int64
}
