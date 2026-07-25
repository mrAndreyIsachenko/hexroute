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
    for identity, user in digitalocean_database_user.runtime :
    identity => format(
      "postgresql://%s:%s@%s:%d/%s?sslmode=require",
      urlencode(user.name),
      urlencode(user.password),
      digitalocean_database_cluster.this.host,
      digitalocean_database_cluster.this.port,
      urlencode(digitalocean_database_db.this.name),
    )
  }
  description = "TLS database URLs for the distinct runtime identities."
  sensitive   = true
}

output "runtime_user_names" {
  value = {
    for identity, user in digitalocean_database_user.runtime :
    identity => user.name
  }
  description = "Deployment login names keyed by fixed application identity."
}

output "bootstrap_database_url" {
  value = format(
    "postgresql://%s:%s@%s:%d/%s?sslmode=require",
    urlencode(digitalocean_database_cluster.this.user),
    urlencode(digitalocean_database_cluster.this.password),
    digitalocean_database_cluster.this.host,
    digitalocean_database_cluster.this.port,
    urlencode(digitalocean_database_db.this.name),
  )
  description = "Bootstrap-only administrator URL; never deliver it to an application component."
  sensitive   = true
}

output "bootstrap_connection" {
  value = {
    host     = digitalocean_database_cluster.this.host
    port     = digitalocean_database_cluster.this.port
    database = digitalocean_database_db.this.name
    user     = digitalocean_database_cluster.this.user
    password = digitalocean_database_cluster.this.password
    sslmode  = "require"
  }
  description = "Bootstrap-only libpq fields for secret-safe operator tooling."
  sensitive   = true
}
