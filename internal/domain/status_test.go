package domain

import (
	"errors"
	"testing"
)

func TestErrorsNeverBecomeNotFound(t *testing.T) {
	for _, err := range []error{
		errors.New("network timeout"),
		errors.New("captcha required"),
		errors.New("selector contract changed"),
	} {
		if got := ClassifyError(err); got == NotFound {
			t.Fatalf("%q was incorrectly classified as not found", err)
		}
	}
	if got := ClassifyError(errors.New("selector missing")); got != FatalError {
		t.Fatalf("selector error = %s, want fatal_error", got)
	}
}

func TestProjectStatusPrecedence(t *testing.T) {
	if got := ProjectStatusFor([]CheckStatus{Found, NeedsReview}); got != ProjectNeedsReview {
		t.Fatalf("status = %s", got)
	}
	if got := ProjectStatusFor([]CheckStatus{NotFound, Found}); got != ProjectCompleted {
		t.Fatalf("status = %s", got)
	}
	if got := ProjectStatusFor([]CheckStatus{RetryableError, Pending, Running}); got != ProjectRunning {
		t.Fatalf("active work must take priority, status = %s", got)
	}
}
