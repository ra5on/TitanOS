package tmail

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/emersion/go-smtp"
)

func TestSMTPSubmissionBudgetPreservesAcceptanceBoundary(t *testing.T) {
	for _, cause := range []error{io.EOF, ErrTLS, ErrTooLarge, context.DeadlineExceeded} {
		err := smtpBudgetOutcome(true, smtpSubmissionError(t.Context(), cause), true)
		if !errors.Is(err, ErrSMTPUnconfirmed) {
			t.Fatal("submission uncertainty was lost", cause, err)
		}
	}
	rejected := smtpSubmissionError(t.Context(), &smtp.SMTPError{Code: 554, Message: "fixture rejected DATA"})
	if err := smtpBudgetOutcome(true, rejected, true); !errors.Is(err, ErrSMTPRejected) || errors.Is(err, ErrSMTPUnconfirmed) {
		t.Fatal(err)
	}
	if err := smtpBudgetOutcome(true, nil, true); err != nil {
		t.Fatal("QUIT response invalidated confirmed DATA", err)
	}
	if err := smtpBudgetOutcome(true, ErrTLS, true); !errors.Is(err, ErrTooLarge) || errors.Is(err, ErrSMTPUnconfirmed) {
		t.Fatal("pre-DATA failure", err)
	}
}
