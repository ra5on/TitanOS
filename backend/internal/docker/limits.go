package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Time limits of docker compose (decided for 1.03): pulling the images of
// an app may take up to 30 minutes, bringing it up or taking it down up to
// 10 each. A registry or a build that hangs then ends the app's job as
// failed instead of keeping it "running" until titand restarts, which held
// back app backups, cleaning up, automatic updates, changes of the app and
// app updates of titand. Variables for tests.
var (
	composePullLimit = 30 * time.Minute
	composeUpLimit   = 10 * time.Minute
	composeDownLimit = 10 * time.Minute
)

// ErrComposeTimeout: docker compose did not finish within its limit and
// was stopped.
var ErrComposeTimeout = errors.New("docker: docker compose did not finish in time")

// JobCodeTimeout is the code of a job that ended at a time limit.
const JobCodeTimeout = "timeout"

// withLimit runs one docker compose command with its own time limit. A
// command cut off by that limit (not by ctx) is reported as
// ErrComposeTimeout with what and the limit.
func withLimit(ctx context.Context, limit time.Duration, what string, run func(context.Context) (string, error)) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, err := run(cctx)
	if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return out, fmt.Errorf("%w: %s", ErrComposeTimeout, timeoutLine(what, limit))
	}
	return out, err
}

// timeoutLine says in the output of a job why it ended.
func timeoutLine(what string, limit time.Duration) string {
	return fmt.Sprintf("TitanOS stopped \"docker compose %s\" after %s: it did not finish within its time limit.", what, minutes(limit))
}

func minutes(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		n := int(d / time.Minute)
		if n == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", n)
	}
	return strings.TrimSpace(d.String())
}

// timeoutText is the line a job's output ends with after a time limit.
func timeoutText(err error) string {
	return strings.TrimPrefix(err.Error(), ErrComposeTimeout.Error()+": ")
}
