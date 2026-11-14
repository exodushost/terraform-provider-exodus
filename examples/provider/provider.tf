terraform {
  required_providers {
    exodus = {
      source = "exodushost/exodus"
    }
  }
}

# The token comes from the EXODUS_TOKEN environment variable.
# Create one in the exodus.host panel: API tokens («API-токены»).
provider "exodus" {}
