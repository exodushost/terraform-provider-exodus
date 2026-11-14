package client

import (
	"net"
	"strconv"
	"strings"
)

// The API stores firewall rules and SSH keys in a canonical form. These
// mirror that, so the provider can tell "written differently" from "changed".

// NormalizePublicKey is "type base64 comment" with single spaces, as the API
// stores it (a trailing newline from file() disappears).
func NormalizePublicKey(key string) string {
	return strings.Join(strings.Fields(key), " ")
}

// NormalizeRule mirrors the API: lower-case protocol, ports "80-443, 22" →
// "80:443,22", source CIDR truncated, "0.0.0.0/0" or empty → none,
// description trimmed to 100 characters. Input the API would reject is
// returned as is.
func NormalizeRule(r Rule) Rule {
	out := Rule{Protocol: strings.ToLower(strings.TrimSpace(r.Protocol))}
	if r.Ports != nil {
		if p := normalizePorts(*r.Ports); p != "" {
			out.Ports = &p
		}
	}
	if r.Source != nil {
		if s := normalizeSource(*r.Source); s != "" {
			out.Source = &s
		}
	}
	desc := []rune(strings.TrimSpace(r.Description))
	if len(desc) > 100 {
		desc = desc[:100]
	}
	out.Description = string(desc)
	return out
}

func normalizePorts(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	var parts []string
	for _, part := range strings.Split(p, ",") {
		part = strings.TrimSpace(part)
		a, b, found := strings.Cut(part, "-")
		if !found {
			a, b, found = strings.Cut(part, ":")
		}
		if !found {
			b = a
		}
		x, err1 := strconv.Atoi(strings.TrimSpace(a))
		y, err2 := strconv.Atoi(strings.TrimSpace(b))
		if err1 != nil || err2 != nil {
			return p
		}
		if x == y {
			parts = append(parts, strconv.Itoa(x))
		} else {
			parts = append(parts, strconv.Itoa(x)+":"+strconv.Itoa(y))
		}
	}
	return strings.Join(parts, ",")
}

func normalizeSource(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil || n.IP.To4() == nil {
			return s
		}
		if ones, _ := n.Mask.Size(); ones == 0 {
			return ""
		}
		return n.String()
	}
	if ip := net.ParseIP(s).To4(); ip != nil {
		return ip.String()
	}
	return s
}

// RulesEqual compares two rule lists after normalisation, order included
// (the API keeps the order it was given).
func RulesEqual(a, b []Rule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := NormalizeRule(a[i]), NormalizeRule(b[i])
		if x.Protocol != y.Protocol || x.Description != y.Description ||
			deref(x.Ports) != deref(y.Ports) || deref(x.Source) != deref(y.Source) {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
