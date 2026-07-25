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
  type        = set(string)
  description = "Distinct runtime login names; SQL migrations grant group roles."
  default = [
    "hexroute_dashboard",
    "hexroute_dashboard_auth",
    "hexroute_ingest",
    "hexroute_maintenance",
    "hexroute_migrator",
  ]

  validation {
    condition = (
      length(var.runtime_users) >= 5 &&
      alltrue([
        for name in var.runtime_users :
        can(regex("^[a-z][a-z0-9_]{2,62}$", name))
      ])
    )
    error_message = "at least five valid, distinct runtime users are required."
  }
}

variable "firewall_rules" {
  type = set(object({
    type  = string
    value = string
  }))
  description = "Trusted-source rules; at least one is required."

  validation {
    condition = (
      length(var.firewall_rules) > 0 &&
      alltrue([
        for rule in var.firewall_rules :
        contains(["app", "droplet", "ip_addr", "k8s", "tag"], rule.type) &&
        length(rule.value) > 0
      ])
    )
    error_message = "at least one valid database firewall rule is required."
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
