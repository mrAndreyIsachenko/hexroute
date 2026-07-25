variable "name" {
  type        = string
  description = "Globally unique private Spaces bucket name."
}

variable "region" {
  type        = string
  description = "Spaces region slug."
}

variable "retention_days" {
  type        = number
  description = "Maximum object and non-current-version retention."
  default     = 30

  validation {
    condition     = var.retention_days >= 1 && var.retention_days <= 30
    error_message = "retention_days must be between 1 and 30."
  }
}

variable "create_runtime_key" {
  type        = bool
  description = "Create one bucket-scoped read/write runtime key."
  default     = true
}

variable "runtime_key_name" {
  type        = string
  description = "Name of the bucket-scoped runtime key."
  default     = "hexroute-incident-bundles"
}

variable "project_id" {
  type        = string
  description = "Optional DigitalOcean project identifier."
  default     = null
  nullable    = true
}
