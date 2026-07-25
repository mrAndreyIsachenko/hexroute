output "check_ids" {
  value = {
    for name, check in digitalocean_uptime_check.this : name => check.id
  }
  description = "Uptime check identifiers keyed by logical name."
}
