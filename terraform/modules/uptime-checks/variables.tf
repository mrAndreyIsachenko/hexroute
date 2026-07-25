variable "checks" {
  type = map(object({
    name    = string
    target  = string
    regions = set(string)
    enabled = optional(bool, true)
  }))
  description = "HTTPS uptime checks without provider-default email alerts."

  validation {
    condition = alltrue([
      for check in values(var.checks) :
      startswith(check.target, "https://") &&
      length(check.regions) >= 2 &&
      alltrue([
        for region in check.regions :
        contains(["eu_west", "se_asia", "us_east", "us_west"], region)
      ])
    ])
    error_message = "checks require HTTPS and at least two supported regions."
  }
}
