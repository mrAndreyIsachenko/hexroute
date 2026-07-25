terraform {
  required_version = ">= 1.8, < 2.0"

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = ">= 2.95, < 3.0"
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
    api = {
      name    = "hexroute-example-api"
      target  = "https://status.example.invalid/readyz"
      regions = ["eu_west", "us_east"]
    }
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

output "contract" {
  value = {
    app_urn                     = module.app_platform.urn
    app_default_ingress         = module.app_platform.default_ingress
    database_urn                = module.managed_postgresql.cluster_urn
    incident_bucket             = module.private_spaces.bucket_name
    dns_records                 = module.dns_records.fqdns
    uptime_checks               = module.uptime_checks.check_ids
    independent_failure_domains = module.ingress_hosts.independent_failure_domains
  }
}
