variable "domain_name" {
  type        = string
  description = "DNS zone name."

  validation {
    condition = (
      can(regex("^[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.domain_name)) &&
      strcontains(var.domain_name, ".") &&
      !strcontains(var.domain_name, "..")
    )
    error_message = "domain_name must be a lowercase absolute DNS name."
  }
}

variable "create_zone" {
  type        = bool
  description = "Create the DigitalOcean DNS zone when true."
  default     = false
}

variable "records" {
  type = map(object({
    type     = string
    name     = string
    value    = string
    ttl      = optional(number, 300)
    priority = optional(number)
    port     = optional(number)
    weight   = optional(number)
    flags    = optional(number)
    tag      = optional(string)
  }))
  description = "DNS records keyed by stable logical name."

  validation {
    condition = alltrue([
      for record in values(var.records) :
      contains(["A", "AAAA", "CAA", "CNAME", "MX", "NS", "SRV", "TXT"], record.type) &&
      record.ttl >= 30 &&
      record.ttl <= 86400
    ])
    error_message = "records must use supported types and bounded TTLs."
  }
}
