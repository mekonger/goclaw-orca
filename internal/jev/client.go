package jev

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

const (
	DefaultAPIBase   = "https://openrouter.ai/api/v1"
	DefaultModel     = "typesafe/jev-1.13"
	DefaultThreshold = 0.6
	requestTimeout   = 10 * time.Second
	questionKey      = "route"
	maxReplyBytes    = 1 << 20
)

var retryDelay = 500 * time.Millisecond

var ErrContract = errors.New("jev reply does not match the expected shape")

type Question struct {
	Instructions string
	Criteria     map[string]string
}

type Answer struct {
	Choice     string
	Confidence float64
	Cost       float64
	Model      string
}

type Client struct {
	endpoint  string
	apiKey    string
	model     string
	threshold float64
	http      *http.Client
}

func NewClient(endpoint, apiKey, model string, threshold float64, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: requestTimeout}
	}
	return &Client{endpoint: endpoint, apiKey: apiKey, model: model, threshold: threshold, http: hc}
}

func (c *Client) Threshold() float64 { return c.threshold }
func (c *Client) Model() string       { return c.model }

type choiceQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type reply struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type       string   `json:"type"`
		Choice     string   `json:"choice"`
		Confidence *float64 `json:"confidence"`
	} `json:"answers"`
	Usage struct {
		Cost float64 `json:"cost"`
	} `json:"usage"`
}

func (c *Client) Choose(ctx context.Context, state map[string]any, q Question) (Answer, error) {
	if len(q.Criteria) == 0 {
		return Answer{}, errors.New("jev question has no criteria")
	}
	if state == nil {
		state = map[string]any{}
	}
	stateJSON, err := marshalNoEscape(state)
	if err != nil {
		return Answer{}, fmt.Errorf("encode jev state: %w", err)
	}
	body, err := marshalNoEscape(map[string]any{
		"model":     c.model,
		"state":     string(stateJSON),
		"questions": map[string]choiceQuestion{questionKey: {Type: "choice", Instructions: q.Instructions, Criteria: q.Criteria}},
	})
	if err != nil {
		return Answer{}, fmt.Errorf("encode jev request: %w", err)
	}
	raw, err := c.post(ctx, body)
	if err != nil {
		return Answer{}, err
	}
	var r reply
	if err := json.Unmarshal(raw, &r); err != nil {
		return Answer{}, ErrContract
	}
	a, ok := r.Answers[questionKey]
	if !ok || a.Type != "choice" || a.Confidence == nil || *a.Confidence < 0 || *a.Confidence > 1 {
		return Answer{}, ErrContract
	}
	if _, known := q.Criteria[a.Choice]; !known {
		return Answer{}, fmt.Errorf("%w: unknown choice", ErrContract)
	}
	return Answer{Choice: a.Choice, Confidence: *a.Confidence, Cost: r.Usage.Cost, Model: r.Model}, nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func (c *Client) post(ctx context.Context, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		raw, retry, err := c.postOnce(ctx, body)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !retry || attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return nil, lastErr
}

func (c *Client) postOnce(ctx context.Context, body []byte) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("build jev request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, fmt.Errorf("jev request: %w", ctx.Err())
		}
		var ne interface{ Timeout() bool }
		return nil, errors.As(err, &ne) && ne.Timeout(), errors.New("jev request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if err != nil {
		return nil, true, errors.New("jev reply read failed")
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return raw, false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("jev returned HTTP %d", resp.StatusCode)
	default:
		return nil, false, fmt.Errorf("jev returned HTTP %d", resp.StatusCode)
	}
}
