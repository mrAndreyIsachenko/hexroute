resource "digitalocean_database_cluster" "this" {
  name       = var.name
  engine     = "pg"
  version    = var.postgresql_version
  size       = var.size
  region     = var.region
  node_count = var.node_count
  project_id = var.project_id
  tags       = var.tags

  maintenance_window {
    day  = var.maintenance_window.day
    hour = var.maintenance_window.hour
  }

  storage_autoscale {
    enabled           = true
    threshold_percent = 80
    increment_gib     = 10
  }
}

resource "digitalocean_database_db" "this" {
  cluster_id = digitalocean_database_cluster.this.id
  name       = var.database_name
}

resource "digitalocean_database_user" "runtime" {
  for_each = var.runtime_users

  cluster_id = digitalocean_database_cluster.this.id
  name       = each.value
}

resource "digitalocean_database_firewall" "this" {
  cluster_id = digitalocean_database_cluster.this.id

  dynamic "rule" {
    for_each = var.firewall_rules
    content {
      type  = rule.value.type
      value = rule.value.value
    }
  }
}
