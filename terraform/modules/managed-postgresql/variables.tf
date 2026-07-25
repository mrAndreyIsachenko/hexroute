variable "name" {
  type        = string
  description = "Managed PostgreSQL cluster name."
}

variable "region" {
  type        = string
  description = "DigitalOcean database region slug."
}

variable "postgresql_version" {
  type        = string
  description = "PostgreSQL major version."
  default     = "17"
}

variable "size" {
  type        = string
  description = "Managed database size slug."
}

variable "node_count" {
  type        = number
  description = "Database node count."
  default     = 1

  validation {
    condition     = var.node_count >= 1 && var.node_count <= 3
    error_message = "node_count must be between 1 and 3."
  }
}

variable "database_name" {
  type        = string
  description = "Application database name."
  default     = "hexroute"
}

variable "runtime_users" {
  type        = map(string)
  description = "Distinct deployment login names keyed by fixed application identity; SQL bootstrap grants the matching NOLOGIN group roles."
  default = {
    dashboard      = "hexroute_dashboard_runtime"
    dashboard_auth = "hexroute_dashboard_auth_runtime"
    ingest         = "hexroute_ingest_runtime"
    maintenance    = "hexroute_maintenance_runtime"
    migrator       = "hexroute_migrator_runtime"
  }

  validation {
    condition = (
      length(setsubtract(
        toset(keys(var.runtime_users)),
        toset(["dashboard", "dashboard_auth", "ingest", "maintenance", "migrator"]),
      )) == 0 &&
      length(setsubtract(
        toset(["dashboard", "dashboard_auth", "ingest", "maintenance", "migrator"]),
        toset(keys(var.runtime_users)),
      )) == 0 &&
      length(toset(values(var.runtime_users))) == 5 &&
      alltrue([
        for name in values(var.runtime_users) :
        can(regex("^[a-z][a-z0-9_]{2,62}$", name))
      ])
    )
    error_message = "all five application identities require valid, distinct deployment login names."
  }
}

variable "firewall_rules" {
  type = set(object({
    type  = string
    value = string
  }))
  description = "Trusted-source rules. An empty set defers firewall creation until the App Platform ID exists."
  default     = []

  validation {
    condition = (
      alltrue([
        for rule in var.firewall_rules :
        contains(["app", "droplet", "ip_addr", "k8s", "tag"], rule.type) &&
        length(rule.value) > 0
      ])
    )
    error_message = "every database firewall rule must have a supported type and non-empty value."
  }
}

variable "project_id" {
  type        = string
  description = "Optional DigitalOcean project identifier."
  default     = null
  nullable    = true
}

variable "tags" {
  type        = set(string)
  description = "Provider tags applied to the cluster."
  default     = []
}

variable "maintenance_window" {
  type = object({
    day  = string
    hour = string
  })
  description = "Weekly UTC maintenance window."
  default = {
    day  = "sunday"
    hour = "03:00"
  }
}
