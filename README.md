# Terraform provider for exodus.host

Manage [exodus.host](https://exodus.host) servers, SSH keys, firewalls and private networks (VPC) with Terraform or OpenTofu.

```hcl
terraform {
  required_providers {
    exodus = { source = "exodushost/exodus" }
  }
}

provider "exodus" {} # token from EXODUS_TOKEN

resource "exodus_ssh_key" "laptop" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "exodus_server" "web" {
  name        = "web"
  region      = "ams"
  image       = "ubuntu-24.04"
  vcpu        = 2
  ram_gb      = 4
  disk_gb     = 50
  ssh_key_ids = [exodus_ssh_key.laptop.id]
}

output "ssh" {
  value = "ssh -p ${exodus_server.web.ssh_port} root@${exodus_server.web.public_ip}"
}
```

## Token

Create an API token in the panel (API tokens, «API-токены») and pass it in an environment variable:

```sh
export EXODUS_TOKEN=exo_...
terraform apply
```

A token belongs to one project, so you don't need to set the project.

## Resources

- `exodus_server`: changing `vcpu` or `ram_gb` restarts the server; `disk_gb` can only grow
- `exodus_ssh_key`
- `exodus_firewall`
- `exodus_vpc`

Everything can be imported by id: `terraform import exodus_server.web <id>`.

Regions, images and sizes: `curl https://exodus.host/api/v1/catalog`. API docs: [exodus.host/api/v1/docs](https://exodus.host/api/v1/docs).

Volumes and snapshots aren't supported yet; use the panel, the API or the [exodus CLI](https://github.com/exodushost/exodus).

## Development

```sh
go test ./...                                              # unit tests
TF_ACC=1 EXODUS_TOKEN=exo_... go test ./internal/provider  # creates a small real server (costs money)
go generate ./...                                          # regenerate docs/
```

To release, push a `vX.Y.Z` tag. GitHub Actions builds and signs the release, and the Terraform Registry picks it up.
