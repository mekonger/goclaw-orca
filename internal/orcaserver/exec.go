package orcaserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"
)

var errCLIMissing = errors.New("orca CLI not found on gateway PATH")

type cliResult struct {
	stdout   []byte
	stderr   string
	runErr   error
	timedOut bool
}

func (c *Checker) timeout() time.Duration {
	if c.Timeout <= 0 {
		return DefaultCheckTimeout
	}
	return c.Timeout
}

func (c *Checker) run(ctx context.Context, code string, args ...string) (cliResult, error) {
	path, err := exec.LookPath("orca")
	if err != nil {
		return cliResult{}, errCLIMissing
	}
	cctx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	cmd := exec.CommandContext(cctx, path, args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), pairingEnv + "=" + code}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr cappedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return cliResult{
		stdout:   stdout.buf.Bytes(),
		stderr:   stderr.buf.String(),
		runErr:   runErr,
		timedOut: errors.Is(cctx.Err(), context.DeadlineExceeded),
	}, nil
}
