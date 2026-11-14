package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func ptr(s string) *string { return &s }

func TestInitAndAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer exo_test" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":"invalid_token","message":"API-токен не найден или удалён"}}`))
			return
		}
		if r.Header.Get("User-Agent") != "terraform-provider-exodus/test" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/api/v1/token":
			json.NewEncoder(w).Encode(TokenInfo{ID: "t1", Name: "ci", ProjectID: "p1", ProjectName: "Мой проект"})
		case "/api/v1/projects/p1/servers/s1":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"code":"not_found","message":"Не найдено"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/api/v1/", "exo_test", "terraform-provider-exodus/test")
	info, err := c.Init(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ProjectID != "p1" || c.ProjectID != "p1" {
		t.Fatalf("project = %q", c.ProjectID)
	}
	_, err = c.GetServer(context.Background(), "s1")
	if !IsNotFound(err) {
		t.Fatalf("want 404, got %v", err)
	}

	bad := New(srv.URL+"/api/v1", "exo_wrong", "terraform-provider-exodus/test")
	_, err = bad.Init(context.Background())
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 401 || apiErr.Code != "invalid_token" {
		t.Fatalf("want invalid_token, got %v", err)
	}
}

func TestRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["action"] != "reboot" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "exo_test", "")
	c.ProjectID = "p1"
	if err := c.ServerAction(context.Background(), "s1", "reboot"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestFindInList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"a","name":"web","rules":[{"protocol":"tcp","ports":"22","source":null,"description":""}]}]`))
	}))
	defer srv.Close()
	c := New(srv.URL, "exo_test", "")
	c.ProjectID = "p1"
	f, err := c.GetFirewall(context.Background(), "a")
	if err != nil || f.Name != "web" || len(f.Rules) != 1 || *f.Rules[0].Ports != "22" {
		t.Fatalf("got %+v, %v", f, err)
	}
	if _, err := c.GetFirewall(context.Background(), "b"); !IsNotFound(err) {
		t.Fatalf("want 404, got %v", err)
	}
}

func TestNormalizeRule(t *testing.T) {
	// The same cases as the API's own normalisation test.
	r := NormalizeRule(Rule{Protocol: "TCP", Ports: ptr("80-443, 22"), Source: ptr("10.0.0.5/8")})
	if *r.Ports != "80:443,22" || *r.Source != "10.0.0.0/8" || r.Protocol != "tcp" {
		t.Fatalf("got %+v", r)
	}
	if NormalizeRule(Rule{Protocol: "tcp", Source: ptr("0.0.0.0/0")}).Source != nil {
		t.Fatal("0.0.0.0/0 should mean anywhere")
	}
	if NormalizeRule(Rule{Protocol: "tcp", Ports: ptr("  ")}).Ports != nil {
		t.Fatal("blank ports should mean all")
	}
	if !RulesEqual(
		[]Rule{{Protocol: "tcp", Ports: ptr("80-443")}, {Protocol: "icmp", Source: ptr("0.0.0.0/0")}},
		[]Rule{{Protocol: "tcp", Ports: ptr("80:443")}, {Protocol: "icmp"}},
	) {
		t.Fatal("rules should be equal")
	}
	if RulesEqual([]Rule{{Protocol: "tcp", Ports: ptr("22")}}, []Rule{{Protocol: "tcp", Ports: ptr("2222")}}) {
		t.Fatal("rules should differ")
	}
}

func TestNormalizePublicKey(t *testing.T) {
	in := "ssh-ed25519  AAAAC3Nza  me@laptop\n"
	if got := NormalizePublicKey(in); got != "ssh-ed25519 AAAAC3Nza me@laptop" {
		t.Fatalf("got %q", got)
	}
}
