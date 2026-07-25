resource "digitalocean_domain" "this" {
  count = var.create_zone ? 1 : 0

  name = var.domain_name
}

resource "digitalocean_record" "this" {
  for_each = var.records

  domain   = var.domain_name
  type     = each.value.type
  name     = each.value.name
  value    = each.value.value
  ttl      = each.value.ttl
  priority = each.value.priority
  port     = each.value.port
  weight   = each.value.weight
  flags    = each.value.flags
  tag      = each.value.tag

  depends_on = [digitalocean_domain.this]
}
