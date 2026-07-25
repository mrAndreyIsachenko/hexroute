locals {
  providers = toset([for host in values(var.hosts) : host.provider])
  asns      = toset([for host in values(var.hosts) : tostring(host.asn)])
}

resource "terraform_data" "host" {
  for_each = var.hosts

  input = {
    provider         = each.value.provider
    asn              = each.value.asn
    region           = each.value.region
    public_hostname  = each.value.public_hostname
    secret_reference = each.value.secret_reference
  }

  lifecycle {
    precondition {
      condition = (
        !var.require_independent_failure_domains ||
        (
          length(var.hosts) >= 2 &&
          length(local.providers) >= 2 &&
          length(local.asns) >= 2
        )
      )
      error_message = "production failover requires at least two providers and ASNs."
    }
  }
}
