package orcaserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	DefaultCheckTimeout = 20 * time.Second
	maxOutputBytes      = 64 << 10
	maxErrorRunes       = 300
	stateReady          = "ready"
	pairingEnv          = "ORCA_PAIRING_CODE"
)

type Checker struct {
	Timeout time.Duration
}

func NewChecker() *Checker {
	return &Checker{Timeout: DefaultCheckTimeout}
}

type cappedBuffer struct {
	buf bytes.Buffer
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxOutputBytes - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

type statusOutput struct {
	Result struct {
		Runtime struct {
			State string `json:"state"`
		} `json:"runtime"`
	} `json:"result"`
	Runtime struct {
		State string `json:"state"`
	} `json:"runtime"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Checker) Check(ctx context.Context, code string) store.OrcaServerHealth {
	h := store.OrcaServerHealth{CheckedAt: time.Now().UTC()}
	res, err := c.run(ctx, code, "status", "--json")
	if err != nil {
		h.Status = store.OrcaServerStatusCLIMissing
		h.Error = err.Error()
		return h
	}

	var out statusOutput
	parseErr := json.Unmarshal(res.stdout, &out)
	state := out.Result.Runtime.State
	if state == "" {
		state = out.Runtime.State
	}

	switch {
	case res.timedOut:
		h.Status = store.OrcaServerStatusError
		h.Error = fmt.Sprintf("orca status timed out after %s", c.timeout())
	case parseErr == nil && state == stateReady:
		h.Status = store.OrcaServerStatusHealthy
		h.RuntimeState = state
		h.Healthy = true
	case parseErr == nil && state != "":
		h.Status = store.OrcaServerStatusUnhealthy
		h.RuntimeState = state
		h.Error = sanitize(firstNonEmpty(out.Error.Message, res.stderr, "runtime state is "+state), code)
	default:
		h.Status = store.OrcaServerStatusError
		msg := out.Error.Message
		if parseErr != nil {
			msg = firstNonEmpty(res.stderr, errText(res.runErr), "unparseable orca status output")
		}
		h.Error = sanitize(firstNonEmpty(msg, res.stderr, errText(res.runErr), "orca status returned no runtime state"), code)
	}
	return h
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sanitize(text, code string) string {
	if code != "" {
		text = strings.ReplaceAll(text, code, "[redacted]")
	}
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > maxErrorRunes {
		text = string(runes[:maxErrorRunes])
	}
	return text
}
