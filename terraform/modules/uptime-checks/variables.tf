variable "checks" {
  type = map(object({
    name                        = string
    type                        = optional(string, "HTTP")
    target                      = optional(string)
    interval                    = optional(number, 300)
    timeout                     = optional(number, 15)
    enabled                     = optional(bool, true)
    tags                        = optional(set(string), ["hexroute", "production"])
    http_method                 = optional(string, "GET")
    follow_redirections         = optional(bool, false)
    success_http_response_codes = optional(set(string), ["200"])
    check_ssl_errors            = optional(bool, true)
    ssl_expiration_reminder     = optional(bool, true)
    domain_expiration_reminder  = optional(bool, false)
    response_time_threshold_ms  = optional(number)
    keyword = optional(object({
      value            = string
      alert_when       = optional(string, "absent")
      case_insensitive = optional(bool, false)
    }))
    api_assertions = optional(object({
      logic = optional(string, "AND")
      checks = list(object({
        property   = string
        comparison = string
        target     = optional(string)
      }))
    }))
    port         = optional(number)
    grace_period = optional(number)
  }))
  description = "Independent UptimeRobot black-box checks with explicit Telegram delivery."

  validation {
    condition     = length(var.checks) > 0 && length(var.checks) <= 10
    error_message = "checks must contain between one and ten Solo-tier monitors."
  }

  validation {
    condition = alltrue([
      for check in values(var.checks) :
      contains(["HTTP", "KEYWORD", "API", "DNS", "PORT", "HEARTBEAT"], check.type) &&
      (check.type == "HEARTBEAT" ? check.target == null : try(length(trimspace(check.target)) > 0, false)) &&
      (contains(["HTTP", "KEYWORD", "API"], check.type) ? try(startswith(check.target, "https://"), false) : true) &&
      contains([60, 300, 600, 900, 1800, 3600], check.interval) &&
      check.timeout >= 1 && check.timeout <= 30 &&
      contains(["GET", "HEAD"], check.http_method) &&
      (check.response_time_threshold_ms == null || (
        check.response_time_threshold_ms >= 50 &&
        check.response_time_threshold_ms <= 60000
      )) &&
      (check.type == "KEYWORD" ? check.keyword != null : check.keyword == null) &&
      (check.keyword == null || (
        length(trimspace(check.keyword.value)) > 0 &&
        contains(["absent", "present"], check.keyword.alert_when)
      )) &&
      (check.type == "API" ? (
        check.api_assertions != null &&
        contains(["AND", "OR"], check.api_assertions.logic) &&
        length(check.api_assertions.checks) >= 1 &&
        length(check.api_assertions.checks) <= 5
      ) : check.api_assertions == null) &&
      (check.type == "PORT" ? (
        check.port != null && check.port >= 1 && check.port <= 65535
      ) : check.port == null) &&
      (check.type == "HEARTBEAT" ? (
        check.grace_period != null && check.grace_period >= 0
      ) : check.grace_period == null) &&
      length(check.tags) >= 1 &&
      alltrue([
        for tag in check.tags :
        can(regex("^[a-z0-9][a-z0-9-]{0,62}$", tag))
      ])
    ])
    error_message = "checks must satisfy the selected monitor type, supported interval, timeout, threshold and lowercase-tag contract."
  }
}

variable "telegram" {
  type = object({
    name                    = string
    notify_on_recovery      = optional(bool, true)
    ssl_expiration_reminder = optional(bool, true)
  })
  description = "UptimeRobot-managed Telegram integration activated out of band through its managed bot."

  validation {
    condition     = length(var.telegram.name) >= 3
    error_message = "telegram integration requires a name."
  }
}
