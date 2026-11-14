package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/exodushost/terraform-provider-exodus/internal/client"
)

// Server and VPC operations are asynchronous: the API accepts them and the
// status changes while they run.

const (
	operationTimeout = 20 * time.Minute
	pollInterval     = 3 * time.Second
)

func poll(ctx context.Context, what string, check func() (done bool, err error)) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timed out after %s waiting for %s", operationTimeout, what)
			}
			return ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

func lastError(s *string) string {
	if s == nil || *s == "" {
		return "no details"
	}
	return *s
}

// waitServerIdle waits until no operation runs on the server and returns it.
func waitServerIdle(ctx context.Context, c *client.Client, id string) (*client.Server, error) {
	var srv *client.Server
	err := poll(ctx, "server "+id, func() (bool, error) {
		s, err := c.GetServer(ctx, id)
		if err != nil {
			return false, err
		}
		if s.Status == "error" {
			return false, fmt.Errorf("server %s is in error state: %s", id, lastError(s.LastError))
		}
		srv = s
		return !s.Busy(), nil
	})
	return srv, err
}

func waitServerGone(ctx context.Context, c *client.Client, id string) error {
	return poll(ctx, "server "+id+" to be deleted", func() (bool, error) {
		s, err := c.GetServer(ctx, id)
		if client.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if s.Status == "error" {
			return false, fmt.Errorf("deleting server %s failed: %s", id, lastError(s.LastError))
		}
		return s.Status == "deleted", nil
	})
}

func waitVPCReady(ctx context.Context, c *client.Client, id string) (*client.VPC, error) {
	var vpc *client.VPC
	err := poll(ctx, "VPC "+id, func() (bool, error) {
		v, err := c.GetVPC(ctx, id)
		if err != nil {
			return false, err
		}
		if v.Status == "error" {
			return false, fmt.Errorf("VPC %s is in error state: %s", id, lastError(v.LastError))
		}
		vpc = v
		return v.Status == "ready", nil
	})
	return vpc, err
}

func waitVPCGone(ctx context.Context, c *client.Client, id string) error {
	return poll(ctx, "VPC "+id+" to be deleted", func() (bool, error) {
		v, err := c.GetVPC(ctx, id)
		if client.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if v.Status == "error" {
			return false, fmt.Errorf("deleting VPC %s failed: %s", id, lastError(v.LastError))
		}
		return false, nil
	})
}
