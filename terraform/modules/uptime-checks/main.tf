resource "uptimerobot_integration" "telegram" {
  name                     = var.telegram.name
  type                     = "telegram"
  value                    = var.telegram.bot_token
  custom_value             = var.telegram.chat_id
  enable_notifications_for = var.telegram.notify_on_recovery ? 1 : 2
  ssl_expiration_reminder  = var.telegram.ssl_expiration_reminder
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

  config = contains(["API", "DNS"], each.value.type) ? merge(
    {},
    each.value.type == "API" ? {
      api_assertions = each.value.api_assertions
    } : {},
  ) : null

  assigned_alert_contacts = [{
    alert_contact_id = uptimerobot_integration.telegram.id
    threshold        = 0
    recurrence       = 0
  }]
}
