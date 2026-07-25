output "cluster_id" {
  value       = digitalocean_database_cluster.this.id
  description = "Managed PostgreSQL cluster identifier."
}

output "cluster_urn" {
  value       = digitalocean_database_cluster.this.urn
  description = "Managed PostgreSQL cluster URN."
}

output "database_name" {
  value       = digitalocean_database_db.this.name
  description = "Application database name."
}

output "runtime_database_urls" {
  value = {
    for name, user in digitalocean_database_user.runtime :
    name => format(
      "postgresql://%s:%s@%s:%d/%s?sslmode=require",
      urlencode(name),
      urlencode(user.password),
      digitalocean_database_cluster.this.host,
      digitalocean_database_cluster.this.port,
      urlencode(digitalocean_database_db.this.name),
    )
  }
  description = "TLS database URLs for the distinct runtime identities."
  sensitive   = true
}
