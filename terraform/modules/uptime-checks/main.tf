resource "uptimerobot_integration" "telegram" {
  name = var.telegram.name
  type = "telegram"
  # Provider 1.9.3 requires value but does not send it for Telegram. UptimeRobot
  # owns the bot credential and activates the destination through its managed bot.
  value                    = "uptimerobot-managed-telegram"
  enable_notifications_for = var.telegram.notify_on_recovery ? 1 : 2
  ssl_expiration_reminder  = var.telegram.ssl_expiration_reminder

  lifecycle {
    # Activation writes the Telegram destination outside Terraform. Preserve it
    # on later applies instead of clearing the managed-bot association.
    ignore_changes = [custom_value]
  }
}

resource "uptimerobot_monitor" "this" {
  for_each = var.checks

  name                        = each.value.name
  type                        = each.value.type
  url                         = each.value.type == "HEARTBEAT" ? null : each.value.target
  interval                    = each.value.interval
  timeout                     = contains(["DNS", "HEARTBEAT"], each.value.type) ? null : each.value.timeout
  is_paused                   = !each.value.enabled
  http_method_type            = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.http_method : null
  follow_redirections         = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.follow_redirections : null
  check_ssl_errors            = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.check_ssl_errors : null
  ssl_expiration_reminder     = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.ssl_expiration_reminder : null
  domain_expiration_reminder  = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.domain_expiration_reminder : null
  success_http_response_codes = contains(["HTTP", "KEYWORD", "API"], each.value.type) ? each.value.success_http_response_codes : null
  response_time_threshold     = contains(["HTTP", "KEYWORD", "API", "PORT"], each.value.type) ? each.value.response_time_threshold_ms : null
  keyword_value               = each.value.type == "KEYWORD" ? each.value.keyword.value : null
  keyword_type = each.value.type == "KEYWORD" ? (
    each.value.keyword.alert_when == "absent" ? "ALERT_NOT_EXISTS" : "ALERT_EXISTS"
  ) : null
  keyword_case_type = each.value.type == "KEYWORD" ? (
    each.value.keyword.case_insensitive ? "CaseInsensitive" : "CaseSensitive"
  ) : null
  port         = each.value.type == "PORT" ? each.value.port : null
  grace_period = each.value.type == "HEARTBEAT" ? each.value.grace_period : null
  tags         = each.value.tags

  config = each.value.type == "API" ? {
    api_assertions = each.value.api_assertions
  } : null

  assigned_alert_contacts = [{
    alert_contact_id = uptimerobot_integration.telegram.id
    threshold        = 0
    recurrence       = 0
  }]
}
