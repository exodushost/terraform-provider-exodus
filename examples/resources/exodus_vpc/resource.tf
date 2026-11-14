resource "exodus_vpc" "private" {
  name   = "private"
  region = "ams"
  cidr   = "10.20.0.0/24"
}
