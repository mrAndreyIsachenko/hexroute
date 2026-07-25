output "record_ids" {
  value = {
    for name, record in digitalocean_record.this : name => record.id
  }
  description = "Record identifiers keyed by logical name."
}

output "fqdns" {
  value = {
    for name, record in digitalocean_record.this : name => record.fqdn
  }
  description = "Record FQDNs keyed by logical name."
}
