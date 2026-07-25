output "id" {
  value       = digitalocean_app.this.id
  description = "App Platform application identifier."
}

output "urn" {
  value       = digitalocean_app.this.urn
  description = "App Platform application URN."
}

output "live_url" {
  value       = digitalocean_app.this.live_url
  description = "Provider-reported live URL."
}

output "default_ingress" {
  value       = digitalocean_app.this.default_ingress
  description = "Default App Platform ingress used by an external edge proxy."
}
