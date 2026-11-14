resource "exodus_firewall" "web" {
  name = "web"
  rules = [
    { protocol = "tcp", ports = "22", source = "203.0.113.10", description = "office SSH" },
    { protocol = "tcp", ports = "80,443" },
    { protocol = "icmp" },
  ]
}
