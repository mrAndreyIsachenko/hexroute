terraform {
  required_version = ">= 1.8.0, < 2.0.0"

  required_providers {
    uptimerobot = {
      source  = "uptimerobot/uptimerobot"
      version = "1.9.3"
    }
  }
}
