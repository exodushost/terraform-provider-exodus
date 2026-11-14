# Terraform-провайдер exodus.host

Серверы, SSH-ключи, firewall и сети VPC на [exodus.host](https://exodus.host) из Terraform или OpenTofu.

```hcl
terraform {
  required_providers {
    exodus = { source = "exodushost/exodus" }
  }
}

provider "exodus" {} # токен из EXODUS_TOKEN

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

## Токен

Создайте токен в панели, раздел «API-токены», и передайте его через переменную окружения:

```sh
export EXODUS_TOKEN=exo_...
terraform apply
```

Токен действует только в своём проекте, поэтому указывать проект не нужно.

## Ресурсы

| Ресурс | Что меняется без пересоздания |
| --- | --- |
| `exodus_server` | `vcpu`, `ram_gb` (сервер перезапустится), `disk_gb` (только увеличение), `firewall_ids`, `vpc_id` |
| `exodus_ssh_key` | — |
| `exodus_firewall` | `name`, `rules` |
| `exodus_vpc` | — |

Все ресурсы импортируются по id: `terraform import exodus_server.web <id>`.

Регионы, образы и допустимые размеры: `curl https://exodus.host/api/v1/catalog`. Описание REST API: https://exodus.host/api/v1/docs.

Диски и снапшоты провайдер пока не умеет, они есть в панели и в API.

## Разработка

```sh
go test ./...                        # юнит-тесты
TF_ACC=1 EXODUS_TOKEN=exo_... \
  go test ./internal/provider -v     # приёмочные: создают и удаляют маленький сервер, это стоит денег
go generate ./...                    # docs/ для реестра
```

Релиз: тег `vX.Y.Z` → GitHub Actions собирает и подписывает архивы (goreleaser), реестр Terraform подхватывает их сам.
