variable "name" {
  type        = string
  description = "App Platform application name."

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,30}[a-z0-9]$", var.name))
    error_message = "name must be a lowercase DNS-style application name."
  }
}

variable "region" {
  type        = string
  description = "DigitalOcean App Platform region slug."
}

variable "project_id" {
  type        = string
  description = "Optional DigitalOcean project identifier."
  default     = null
  nullable    = true
}

variable "domain" {
  type = object({
    name = string
    zone = optional(string)
  })
  description = "Optional stable primary domain and DigitalOcean DNS zone."
  default     = null
  nullable    = true
}

variable "image" {
  type = object({
    registry_type = string
    registry      = optional(string)
    repository    = string
    digest        = string
  })
  description = "Immutable API and worker image coordinates."

  validation {
    condition = contains(["DOCR", "DOCKER_HUB"], var.image.registry_type) && can(
      regex("^sha256:[0-9a-f]{64}$", var.image.digest)
    )
    error_message = "image must use a supported registry and sha256 digest."
  }

  validation {
    condition = (
      var.image.registry_type == "DOCR" ||
      try(length(var.image.registry) > 0, false)
    )
    error_message = "DOCKER_HUB images require a registry name."
  }
}

variable "api" {
  type = object({
    instance_size_slug = string
    instance_count     = number
    http_port          = optional(number, 8080)
  })
  description = "API component capacity."

  validation {
    condition = (
      var.api.instance_count >= 1 &&
      var.api.instance_count <= 10 &&
      var.api.http_port >= 1024 &&
      var.api.http_port <= 65535
    )
    error_message = "API capacity or port is outside the bounded range."
  }
}

variable "worker" {
  type = object({
    instance_size_slug = string
    instance_count     = number
  })
  description = "Worker component capacity."

  validation {
    condition     = var.worker.instance_count >= 1 && var.worker.instance_count <= 10
    error_message = "worker instance_count must be between 1 and 10."
  }
}

variable "api_environment" {
  type        = map(string)
  description = "Non-secret API runtime environment."
  default     = {}
}

variable "api_secret_environment" {
  type        = map(string)
  description = "API runtime values stored as App Platform secrets."
  default     = {}
  sensitive   = true
}

variable "worker_environment" {
  type        = map(string)
  description = "Non-secret worker runtime environment."
  default     = {}
}

variable "worker_secret_environment" {
  type        = map(string)
  description = "Worker runtime values stored as App Platform secrets."
  default     = {}
  sensitive   = true
}
