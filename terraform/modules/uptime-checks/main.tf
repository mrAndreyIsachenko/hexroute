resource "digitalocean_uptime_check" "this" {
  for_each = var.checks

  name    = each.value.name
  target  = each.value.target
  type    = "https"
  regions = each.value.regions
  enabled = each.value.enabled
}
