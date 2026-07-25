output "bucket_name" {
  value       = digitalocean_spaces_bucket.this.name
  description = "Private bucket name."
}

output "bucket_urn" {
  value       = digitalocean_spaces_bucket.this.urn
  description = "Private bucket URN."
}

output "endpoint" {
  value       = digitalocean_spaces_bucket.this.endpoint
  description = "Regional Spaces endpoint."
}

output "runtime_access_key" {
  value       = try(digitalocean_spaces_key.runtime[0].access_key, null)
  description = "Bucket-scoped runtime access key."
  sensitive   = true
}

output "runtime_secret_key" {
  value       = try(digitalocean_spaces_key.runtime[0].secret_key, null)
  description = "Bucket-scoped runtime secret key."
  sensitive   = true
}
