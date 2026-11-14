package provider

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

// Acceptance tests run against the real API (TF_ACC=1, EXODUS_TOKEN set)
// and create a small billed server for a few minutes.

var regexpSHA256 = regexp.MustCompile(`^SHA256:`)

var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"exodus": providerserver.NewProtocol6WithError(New("test")()),
}

func testAccPreCheck(t *testing.T) {
	if os.Getenv("EXODUS_TOKEN") == "" {
		t.Fatal("EXODUS_TOKEN must be set for acceptance tests")
	}
}

// testPublicKey is a fresh ed25519 key in OpenSSH format, with the trailing
// newline file() would give, to exercise normalisation.
func testPublicKey(t *testing.T) string {
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	var blob []byte
	for _, part := range [][]byte{[]byte("ssh-ed25519"), pub} {
		blob = binary.BigEndian.AppendUint32(blob, uint32(len(part)))
		blob = append(blob, part...)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + " tf-acc\n"
}

func config(name, key string, vcpu, disk int, rules string, withVPC bool) string {
	vpc := ""
	if withVPC {
		vpc = "vpc_id = exodus_vpc.test.id"
	}
	return fmt.Sprintf(`
resource "exodus_ssh_key" "test" {
  name       = %[1]q
  public_key = %[2]q
}

resource "exodus_firewall" "test" {
  name  = %[1]q
  rules = [%[5]s]
}

resource "exodus_vpc" "test" {
  name   = %[1]q
  region = "ams"
}

resource "exodus_server" "test" {
  name         = %[1]q
  region       = "ams"
  image        = "ubuntu-24.04"
  vcpu         = %[3]d
  ram_gb       = 1
  disk_gb      = %[4]d
  ssh_key_ids  = [exodus_ssh_key.test.id]
  firewall_ids = [exodus_firewall.test.id]
  %[6]s
}
`, name, key, vcpu, disk, rules, vpc)
}

func TestAccLifecycle(t *testing.T) {
	name := "tf-acc-" + strings.ToLower(acctest.RandStringFromCharSet(6, acctest.CharSetAlpha))
	key := testPublicKey(t)
	rules1 := `
    { protocol = "TCP", ports = "22" },
    { protocol = "tcp", ports = "8000-8100", source = "10.0.0.5/8", description = "range" },
    { protocol = "icmp", source = "0.0.0.0/0" },`
	rules2 := rules1 + `
    { protocol = "tcp", ports = "80,443" },`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: config(name, key, 1, 20, rules1, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("exodus_server.test", "status", "running"),
					resource.TestCheckResourceAttrSet("exodus_server.test", "public_ip"),
					resource.TestCheckResourceAttrSet("exodus_server.test", "ssh_port"),
					resource.TestCheckResourceAttrSet("exodus_server.test", "root_password"),
					resource.TestCheckResourceAttrPair("exodus_server.test", "vpc_id", "exodus_vpc.test", "id"),
					resource.TestCheckResourceAttrSet("exodus_server.test", "vpc_ip"),
					resource.TestCheckResourceAttr("exodus_vpc.test", "cidr", "10.20.0.0/24"),
					resource.TestMatchResourceAttr("exodus_ssh_key.test", "fingerprint", regexpSHA256),
					resource.TestCheckResourceAttr("exodus_firewall.test", "rules.#", "3"),
					// written as in config, though the API stores "8000:8100", "10.0.0.0/8", no source
					resource.TestCheckResourceAttr("exodus_firewall.test", "rules.1.ports", "8000-8100"),
				),
			},
			{
				// Re-reading must not show differences from the API's canonical forms.
				Config:   config(name, key, 1, 20, rules1, true),
				PlanOnly: true,
			},
			{
				Config: config(name, key, 2, 30, rules2, false),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("exodus_server.test", plancheck.ResourceActionUpdate),
						plancheck.ExpectResourceAction("exodus_firewall.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("exodus_server.test", "vcpu", "2"),
					resource.TestCheckResourceAttr("exodus_server.test", "disk_gb", "30"),
					resource.TestCheckResourceAttr("exodus_server.test", "status", "running"),
					resource.TestCheckNoResourceAttr("exodus_server.test", "vpc_id"),
					resource.TestCheckResourceAttr("exodus_firewall.test", "rules.#", "4"),
				),
			},
			{
				ResourceName:            "exodus_server.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"root_password", "ssh_key_ids"},
			},
			{
				ResourceName:      "exodus_firewall.test",
				ImportState:       true,
				ImportStateVerify: true,
				// imported rules come back in the API's canonical form
				ImportStateVerifyIgnore: []string{"rules"},
			},
		},
	})
}
