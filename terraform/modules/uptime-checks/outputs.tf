output "check_ids" {
  value = {
    for name, check in uptimerobot_monitor.this : name => check.id
  }
  description = "UptimeRobot monitor identifiers keyed by logical name."
}

output "telegram_integration_id" {
  value       = uptimerobot_integration.telegram.id
  description = "UptimeRobot Telegram integration identifier."
}
