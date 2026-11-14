resource "exodus_server" "web" {
  name    = "web"
  region  = "ams"
  image   = "ubuntu-24.04"
  vcpu    = 2
  ram_gb  = 4
  disk_gb = 50

  ssh_key_ids  = [exodus_ssh_key.laptop.id]
  firewall_ids = [exodus_firewall.web.id]
  vpc_id       = exodus_vpc.private.id
}

output "ssh" {
  value = "ssh -p ${exodus_server.web.ssh_port} root@${exodus_server.web.public_ip}"
}
