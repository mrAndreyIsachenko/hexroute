variable "hosts" {
  type = map(object({
    provider         = string
    asn              = number
    region           = string
    public_hostname  = string
    secret_reference = string
  }))
  description = "Provider-neutral ingress inventory supplied only by private roots."

  validation {
    condition = alltrue([
      for host in values(var.hosts) :
      host.asn > 0 &&
      can(regex("^[a-z0-9][a-z0-9.-]+[a-z0-9]$", host.public_hostname)) &&
      startswith(host.secret_reference, "secret://") &&
      length(host.provider) > 0 &&
      length(host.region) > 0
    ])
    error_message = "ingress hosts require bounded metadata and secret references."
  }
}

variable "require_independent_failure_domains" {
  type        = bool
  description = "Require two providers and two ASNs when production failover is declared."
  default     = false
}
