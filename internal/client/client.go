// Package client is a small client for the exodus.host REST API
// (https://exodus.host/api/v1/docs), covering what the provider manages.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the production API.
const DefaultEndpoint = "https://exodus.host/api/v1"

// Client talks to one project: the one the API token belongs to.
type Client struct {
	endpoint  string
	token     string
	userAgent string
	http      *http.Client
	// ProjectID is set by [Client.Init].
	ProjectID string
}

// APIError is a non-2xx answer: {"error": {"code": "...", "message": "..."}}.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("exodus.host API: HTTP %d", e.Status)
	}
	return fmt.Sprintf("exodus.host API: %s (%s, HTTP %d)", e.Message, e.Code, e.Status)
}

// IsNotFound reports whether err is a 404 from the API.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// New returns a client; call [Client.Init] before using it.
func New(endpoint, token, userAgent string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		token:     token,
		userAgent: userAgent,
		http:      &http.Client{Timeout: 60 * time.Second},
	}
}

// TokenInfo describes the API token in use.
type TokenInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

// Init checks the token and learns its project.
func (c *Client) Init(ctx context.Context) (*TokenInfo, error) {
	var t TokenInfo
	if err := c.do(ctx, http.MethodGet, "/token", nil, &t); err != nil {
		return nil, err
	}
	c.ProjectID = t.ProjectID
	return &t, nil
}

func (c *Client) project(path string) string {
	return "/projects/" + c.ProjectID + path
}

// do sends a JSON request and decodes a JSON answer into out (if non-nil).
// Rate-limited requests (429) are retried with a growing pause.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if c.userAgent != "" {
			req.Header.Set("User-Agent", c.userAgent)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 5 {
			wait := time.Duration(attempt+1) * 5 * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			apiErr := &APIError{Status: resp.StatusCode}
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(data, &envelope) == nil {
				apiErr.Code, apiErr.Message = envelope.Error.Code, envelope.Error.Message
			}
			return apiErr
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("decode %s %s: %w", method, path, err)
			}
		}
		return nil
	}
}
