variable "name" {
  type        = string
  description = "Stable provider-neutral name for the Lightsail ingress."

  validation {
    condition = (
      length(var.name) >= 3 &&
      length(var.name) <= 63 &&
      can(regex("^[a-z][a-z0-9-]*[a-z0-9]$", var.name))
    )
    error_message = "name must be 3-63 lowercase alphanumeric or hyphen characters."
  }
}

variable "availability_zone" {
  type        = string
  description = "Availability zone discovered and pinned by the private root."

  validation {
    condition = can(regex(
      "^[a-z]{2}(-gov)?-[a-z]+-[0-9][a-z]$",
      var.availability_zone,
    ))
    error_message = "availability_zone must be a syntactically valid AWS zone."
  }
}

variable "blueprint_id" {
  type        = string
  description = "Linux Lightsail blueprint identifier discovered by the private root."

  validation {
    condition = (
      length(var.blueprint_id) >= 2 &&
      length(var.blueprint_id) <= 128 &&
      can(regex("^[a-z0-9][a-z0-9_.-]+$", var.blueprint_id))
    )
    error_message = "blueprint_id must be a bounded lowercase Lightsail identifier."
  }
}

variable "bundle_id" {
  type        = string
  description = "Lightsail bundle identifier discovered and cost-checked by the private root."

  validation {
    condition = (
      length(var.bundle_id) >= 2 &&
      length(var.bundle_id) <= 128 &&
      can(regex("^[a-z0-9][a-z0-9_.-]+$", var.bundle_id))
    )
    error_message = "bundle_id must be a bounded lowercase Lightsail identifier."
  }
}

variable "key_pair_name" {
  type        = string
  description = "Optional Lightsail key-pair name supplied by the private root."
  default     = null
  nullable    = true

  validation {
    condition = var.key_pair_name == null || (
      length(var.key_pair_name) >= 1 &&
      length(var.key_pair_name) <= 255 &&
      can(regex("^[A-Za-z0-9._-]+$", var.key_pair_name))
    )
    error_message = "key_pair_name must be null or a bounded Lightsail key-pair name."
  }
}

variable "automatic_snapshot_enabled" {
  type        = bool
  description = "Enable the Lightsail automatic snapshot add-on."
  default     = true
}

variable "automatic_snapshot_time_utc" {
  type        = string
  description = "UTC hourly snapshot window in HH:00 form."
  default     = "03:00"

  validation {
    condition     = can(regex("^([01][0-9]|2[0-3]):00$", var.automatic_snapshot_time_utc))
    error_message = "automatic_snapshot_time_utc must be an hourly UTC time in HH:00 form."
  }
}

variable "public_ports" {
  type = list(object({
    protocol  = string
    from_port = number
    to_port   = number
    cidrs     = set(string)
  }))
  description = "Authoritative IPv4 ingress rules: global TCP 443 and optional TCP 22 from /32 networks."
  default = [{
    protocol  = "tcp"
    from_port = 443
    to_port   = 443
    cidrs     = ["0.0.0.0/0"]
  }]

  validation {
    condition = (
      length(var.public_ports) >= 1 &&
      length(var.public_ports) <= 8 &&
      alltrue([
        for rule in var.public_ports :
        rule.protocol == "tcp" &&
        rule.from_port == rule.to_port &&
        contains([22, 443], rule.from_port) &&
        length(rule.cidrs) >= 1 &&
        alltrue([for cidr in rule.cidrs : can(cidrnetmask(cidr))]) &&
        (
          rule.from_port != 22 ||
          alltrue([for cidr in rule.cidrs : endswith(cidr, "/32")])
        )
      ]) &&
      length([
        for rule in var.public_ports : rule
        if rule.from_port == 443 && rule.cidrs == toset(["0.0.0.0/0"])
      ]) == 1
    )
    error_message = "public_ports requires one global TCP 443 rule and permits TCP 22 only from IPv4 /32 networks."
  }
}

variable "tags" {
  type        = map(string)
  description = "Non-secret caller tags; module ownership tags are reserved."
  default     = {}

  validation {
    condition = alltrue([
      for key, value in var.tags :
      !contains(["HexrouteManaged", "HexrouteComponent"], key) &&
      length(key) >= 1 && length(key) <= 128 &&
      length(value) <= 256
    ])
    error_message = "tags must be bounded and cannot replace module ownership tags."
  }
}
