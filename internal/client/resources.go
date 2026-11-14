package client

import (
	"context"
	"net/http"
)

// ── SSH keys ────────────────────────────────────────────────────────────────

type SSHKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
}

func (c *Client) CreateSSHKey(ctx context.Context, name, publicKey string) (*SSHKey, error) {
	var k SSHKey
	body := map[string]string{"name": name, "public_key": publicKey}
	return &k, c.do(ctx, http.MethodPost, c.project("/ssh-keys"), body, &k)
}

// GetSSHKey returns a 404 APIError when the key is gone.
func (c *Client) GetSSHKey(ctx context.Context, id string) (*SSHKey, error) {
	var keys []SSHKey
	if err := c.do(ctx, http.MethodGet, c.project("/ssh-keys"), nil, &keys); err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "SSH key not found"}
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.project("/ssh-keys/"+id), nil, nil)
}

// ── Servers ─────────────────────────────────────────────────────────────────

type Server struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	ImageID     string   `json:"image_id"`
	RegionID    string   `json:"region_id"`
	VCPU        int64    `json:"vcpu"`
	RAMMB       int64    `json:"ram_mb"`
	DiskGB      int64    `json:"disk_gb"`
	PrivateIP   string   `json:"private_ip"`
	PublicIP    *string  `json:"public_ip"`
	SSHPort     *int64   `json:"ssh_port"`
	VPCID       *string  `json:"vpc_id"`
	VPCIP       *string  `json:"vpc_ip"`
	FirewallIDs []string `json:"firewall_ids"`
	LastError   *string  `json:"last_error"`
}

// Busy reports whether an operation on the server is still running.
func (s *Server) Busy() bool {
	switch s.Status {
	case "creating", "starting", "stopping", "rebooting", "resizing", "restoring", "deleting":
		return true
	}
	return false
}

type CreateServerRequest struct {
	Name        string   `json:"name"`
	RegionID    string   `json:"region_id"`
	ImageID     string   `json:"image_id"`
	VCPU        int64    `json:"vcpu"`
	RAMGB       int64    `json:"ram_gb"`
	DiskGB      int64    `json:"disk_gb"`
	SSHKeyIDs   []string `json:"ssh_key_ids"`
	FirewallIDs []string `json:"firewall_ids"`
	VPCID       *string  `json:"vpc_id"`
}

// CreateServer starts creating a server; the root password is only ever
// returned here.
func (c *Client) CreateServer(ctx context.Context, req CreateServerRequest) (*Server, string, error) {
	var out struct {
		Server       Server `json:"server"`
		RootPassword string `json:"root_password"`
	}
	if req.SSHKeyIDs == nil {
		req.SSHKeyIDs = []string{}
	}
	if req.FirewallIDs == nil {
		req.FirewallIDs = []string{}
	}
	if err := c.do(ctx, http.MethodPost, c.project("/servers"), req, &out); err != nil {
		return nil, "", err
	}
	return &out.Server, out.RootPassword, nil
}

// GetServer returns a 404 APIError once the server is deleted.
func (c *Client) GetServer(ctx context.Context, id string) (*Server, error) {
	var s Server
	return &s, c.do(ctx, http.MethodGet, c.project("/servers/"+id), nil, &s)
}

func (c *Client) DeleteServer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.project("/servers/"+id), nil, nil)
}

// ServerAction is one of start, shutdown, stop, reboot.
func (c *Client) ServerAction(ctx context.Context, id, action string) error {
	return c.do(ctx, http.MethodPost, c.project("/servers/"+id+"/actions"), map[string]string{"action": action}, nil)
}

// ResizeServer changes CPU/RAM (server must be stopped) and/or grows the disk.
func (c *Client) ResizeServer(ctx context.Context, id string, vcpu, ramGB, diskGB int64) error {
	body := map[string]int64{"vcpu": vcpu, "ram_gb": ramGB, "disk_gb": diskGB}
	return c.do(ctx, http.MethodPost, c.project("/servers/"+id+"/resize"), body, nil)
}

func (c *Client) SetServerFirewalls(ctx context.Context, id string, firewallIDs []string) error {
	if firewallIDs == nil {
		firewallIDs = []string{}
	}
	body := map[string][]string{"firewall_ids": firewallIDs}
	return c.do(ctx, http.MethodPut, c.project("/servers/"+id+"/firewalls"), body, nil)
}

// SetServerVPC joins vpcID, or leaves the current VPC when vpcID is nil.
func (c *Client) SetServerVPC(ctx context.Context, id string, vpcID *string) error {
	body := map[string]*string{"vpc_id": vpcID}
	return c.do(ctx, http.MethodPut, c.project("/servers/"+id+"/vpc"), body, nil)
}

// ── Firewalls ───────────────────────────────────────────────────────────────

type Rule struct {
	Protocol    string  `json:"protocol"`
	Ports       *string `json:"ports"`
	Source      *string `json:"source"`
	Description string  `json:"description"`
}

type Firewall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

type firewallRequest struct {
	Name  string `json:"name"`
	Rules []Rule `json:"rules"`
}

func (c *Client) CreateFirewall(ctx context.Context, name string, rules []Rule) (*Firewall, error) {
	var f Firewall
	if rules == nil {
		rules = []Rule{}
	}
	return &f, c.do(ctx, http.MethodPost, c.project("/firewalls"), firewallRequest{name, rules}, &f)
}

func (c *Client) UpdateFirewall(ctx context.Context, id, name string, rules []Rule) (*Firewall, error) {
	var f Firewall
	if rules == nil {
		rules = []Rule{}
	}
	return &f, c.do(ctx, http.MethodPut, c.project("/firewalls/"+id), firewallRequest{name, rules}, &f)
}

// GetFirewall returns a 404 APIError when the firewall is gone.
func (c *Client) GetFirewall(ctx context.Context, id string) (*Firewall, error) {
	var all []Firewall
	if err := c.do(ctx, http.MethodGet, c.project("/firewalls"), nil, &all); err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "firewall not found"}
}

func (c *Client) DeleteFirewall(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.project("/firewalls/"+id), nil, nil)
}

// ── VPCs ────────────────────────────────────────────────────────────────────

type VPC struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	RegionID  string  `json:"region_id"`
	CIDR      string  `json:"cidr"`
	Status    string  `json:"status"` // creating, ready, deleting, error
	LastError *string `json:"last_error"`
}

func (c *Client) CreateVPC(ctx context.Context, name, regionID string, cidr *string) (*VPC, error) {
	var v VPC
	body := map[string]*string{"name": &name, "region_id": &regionID, "cidr": cidr}
	return &v, c.do(ctx, http.MethodPost, c.project("/vpcs"), body, &v)
}

// GetVPC returns a 404 APIError once the VPC is deleted.
func (c *Client) GetVPC(ctx context.Context, id string) (*VPC, error) {
	var all []VPC
	if err := c.do(ctx, http.MethodGet, c.project("/vpcs"), nil, &all); err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "VPC not found"}
}

func (c *Client) DeleteVPC(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.project("/vpcs/"+id), nil, nil)
}
