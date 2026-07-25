variable "checks" {
  type = map(object({
    name     = string
    target   = string
    interval = optional(number, 300)
    timeout  = optional(number, 15)
    enabled  = optional(bool, true)
    tags     = optional(set(string), ["hexroute", "production"])
  }))
  description = "Independent UptimeRobot HTTPS checks with explicit Telegram delivery."

  validation {
    condition = alltrue([
      for check in values(var.checks) :
      startswith(check.target, "https://") &&
      contains([60, 300, 600, 900, 1800, 3600], check.interval) &&
      check.timeout >= 1 && check.timeout <= 30 &&
      length(check.tags) >= 1 &&
      alltrue([
        for tag in check.tags :
        can(regex("^[a-z0-9][a-z0-9-]{0,62}$", tag))
      ])
    ])
    error_message = "checks require HTTPS, a supported interval, a 1-30 second timeout and lowercase tags."
  }
}

variable "telegram" {
  type = object({
    name                    = string
    bot_token               = string
    chat_id                 = string
    notify_on_recovery      = optional(bool, true)
    ssl_expiration_reminder = optional(bool, true)
  })
  description = "Telegram integration used directly by UptimeRobot."
  sensitive   = true

  validation {
    condition = (
      length(var.telegram.name) >= 3 &&
      length(var.telegram.bot_token) >= 24 &&
      length(var.telegram.chat_id) >= 1
    )
    error_message = "telegram integration requires a name, bot token and destination."
  }
}
