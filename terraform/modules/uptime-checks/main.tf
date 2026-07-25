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
  type                        = "HTTP"
  url                         = each.value.target
  interval                    = each.value.interval
  timeout                     = each.value.timeout
  is_paused                   = !each.value.enabled
  http_method_type            = "GET"
  follow_redirections         = false
  check_ssl_errors            = true
  ssl_expiration_reminder     = true
  success_http_response_codes = ["200"]
  tags                        = each.value.tags

  assigned_alert_contacts = [{
    alert_contact_id = uptimerobot_integration.telegram.id
    threshold        = 0
    recurrence       = 0
  }]
}
