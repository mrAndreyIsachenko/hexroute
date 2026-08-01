terraform {
  required_version = ">= 1.8, < 2.0"

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = ">= 2.95, < 3.0"
    }
    uptimerobot = {
      source  = "uptimerobot/uptimerobot"
      version = "1.9.3"
    }
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0, < 7.0"
    }
  }
}

module "app_platform" {
  source = "../../modules/app-platform"

  name       = "hexroute-example"
  region     = "ams"
  project_id = "00000000-0000-0000-0000-000000000000"
  domain = {
    name = "status.example.invalid"
    zone = "example.invalid"
  }
  image = {
    registry_type = "DOCR"
    repository    = "hexroute-ingest"
    digest        = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
  }
  api = {
    instance_size_slug = "basic-xxs"
    instance_count     = 1
  }
  worker = {
    instance_size_slug = "basic-xxs"
    instance_count     = 1
  }
  migration = {
    instance_size_slug = "basic-xxs"
  }
  api_environment = {
    HEXROUTE_PUBLIC_ORIGIN = "https://status.example.invalid"
  }
  api_secret_environment = {
    HEXROUTE_DATABASE_URL = "secret://database/api"
  }
  worker_environment = {
    HEXROUTE_RETENTION_DAYS = "30"
  }
  worker_secret_environment = {
    HEXROUTE_DATABASE_URL = "secret://database/worker"
  }
  migration_environment = {
    HEXROUTE_BOOTSTRAP_USERNAME     = "operator"
    HEXROUTE_BOOTSTRAP_DISPLAY_NAME = "Operator"
  }
  migration_secret_environment = {
    HEXROUTE_MIGRATOR_DATABASE_URL = "secret://database/migrator"
  }
}

module "managed_postgresql" {
  source = "../../modules/managed-postgresql"

  name   = "hexroute-example"
  region = "ams3"
  size   = "db-s-1vcpu-1gb"
  firewall_rules = [
    {
      type  = "ip_addr"
      value = "192.0.2.10"
    },
  ]
}

module "private_spaces" {
  source = "../../modules/private-spaces"

  name   = "hexroute-example-invalid"
  region = "ams3"
}

module "dns_records" {
  source = "../../modules/dns-records"

  domain_name = "example.invalid"
  records = {
    status = {
      type  = "CNAME"
      name  = "status"
      value = "example.invalid."
    }
  }
}

module "uptime_checks" {
  source = "../../modules/uptime-checks"

  checks = {
    readiness = {
      name   = "hexroute-example-readiness"
      type   = "API"
      target = "https://status.example.invalid/readyz"
      api_assertions = {
        checks = [{
          property   = "$.status"
          comparison = "equals"
          target     = jsonencode("ready")
        }]
      }
    }
    login = {
      name   = "hexroute-example-login"
      type   = "KEYWORD"
      target = "https://status.example.invalid/login"
      keyword = {
        value      = "Sign in with passkey"
        alert_when = "absent"
      }
    }
    dns = {
      name   = "hexroute-example-dns"
      type   = "DNS"
      target = "status.example.invalid"
    }
    ingress = {
      name   = "hexroute-example-ingress"
      type   = "PORT"
      target = "primary.example.invalid"
      port   = 443
    }
    backup = {
      name         = "hexroute-example-backup"
      type         = "HEARTBEAT"
      target       = null
      interval     = 3600
      grace_period = 900
    }
  }
  telegram = {
    name = "Hexroute example alerts"
  }
}

module "ingress_hosts" {
  source = "../../modules/ingress-hosts"

  require_independent_failure_domains = true
  hosts = {
    primary = {
      provider         = "provider-a"
      asn              = 64500
      region           = "eu-a"
      public_hostname  = "primary.example.invalid"
      secret_reference = "secret://provider-a/ingress"
    }
    secondary = {
      provider         = "provider-b"
      asn              = 64501
      region           = "eu-b"
      public_hostname  = "secondary.example.invalid"
      secret_reference = "secret://provider-b/ingress"
    }
  }
}

module "lightsail_ingress" {
  source = "../../modules/lightsail-ingress"

  name              = "hexroute-example-ingress"
  availability_zone = "us-east-1a"
  blueprint_id      = "ubuntu_24_04"
  bundle_id         = "micro_3_0"
  tags = {
    Environment = "synthetic"
  }
}

output "contract" {
  value = {
    app_urn                     = module.app_platform.urn
    app_default_ingress         = module.app_platform.default_ingress
    database_urn                = module.managed_postgresql.cluster_urn
    incident_bucket             = module.private_spaces.bucket_name
    dns_records                 = module.dns_records.fqdns
    uptime_checks               = module.uptime_checks.check_ids
    uptime_telegram_integration = module.uptime_checks.telegram_integration_id
    independent_failure_domains = module.ingress_hosts.independent_failure_domains
    lightsail_instance_name     = module.lightsail_ingress.instance_name
    lightsail_firewall_rules    = module.lightsail_ingress.firewall_rules
  }
}
