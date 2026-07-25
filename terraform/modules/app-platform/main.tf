locals {
  forbidden_management_keys = toset([
    "DIGITALOCEAN_ACCESS_TOKEN",
    "DIGITALOCEAN_TOKEN",
    "DO_API_KEY",
    "DO_API_TOKEN",
    "DO_OAUTH_TOKEN",
    "DO_TOKEN",
    "CLOUDFLARE_API_TOKEN",
    "OAUTH_TOKEN",
    "SPACES_ACCESS_KEY_ID",
    "SPACES_SECRET_ACCESS_KEY",
    "TF_TOKEN_app_terraform_io",
  ])
  forbidden_api_spaces_keys = toset([
    "HEXROUTE_INCIDENT_SPACES_ACCESS_KEY_ID",
    "HEXROUTE_INCIDENT_SPACES_SECRET_ACCESS_KEY",
  ])
  reserved_component_keys = toset([
    "HEXROUTE_COMPONENT",
  ])
  api_keys = setunion(
    toset(keys(var.api_environment)),
    toset(nonsensitive(keys(var.api_secret_environment))),
  )
  worker_keys = setunion(
    toset(keys(var.worker_environment)),
    toset(nonsensitive(keys(var.worker_secret_environment))),
  )
}

resource "digitalocean_app" "this" {
  project_id = var.project_id

  spec {
    name                            = var.name
    region                          = var.region
    disable_edge_cache              = var.domain == null
    enhanced_threat_control_enabled = var.domain != null

    dynamic "domain" {
      for_each = var.domain == null ? [] : [var.domain]
      content {
        name = domain.value.name
        type = "PRIMARY"
        zone = domain.value.zone
      }
    }

    service {
      name               = "api"
      instance_count     = var.api.instance_count
      instance_size_slug = var.api.instance_size_slug
      http_port          = var.api.http_port

      image {
        registry_type = var.image.registry_type
        registry      = var.image.registry
        repository    = var.image.repository
        digest        = var.image.digest

        deploy_on_push {
          enabled = false
        }
      }

      env {
        key   = "HEXROUTE_COMPONENT"
        value = "api"
        scope = "RUN_TIME"
        type  = "GENERAL"
      }

      dynamic "env" {
        for_each = var.api_environment
        content {
          key   = env.key
          value = env.value
          scope = "RUN_TIME"
          type  = "GENERAL"
        }
      }

      dynamic "env" {
        for_each = nonsensitive(toset(keys(var.api_secret_environment)))
        content {
          key   = env.value
          value = var.api_secret_environment[env.value]
          scope = "RUN_TIME"
          type  = "SECRET"
        }
      }

      health_check {
        http_path             = "/readyz"
        initial_delay_seconds = 10
        period_seconds        = 10
        timeout_seconds       = 3
        success_threshold     = 1
        failure_threshold     = 3
      }

      liveness_health_check {
        http_path             = "/livez"
        initial_delay_seconds = 10
        period_seconds        = 10
        timeout_seconds       = 3
        success_threshold     = 1
        failure_threshold     = 3
      }

      termination {
        drain_seconds        = 5
        grace_period_seconds = 20
      }
    }

    worker {
      name               = "worker"
      instance_count     = var.worker.instance_count
      instance_size_slug = var.worker.instance_size_slug

      image {
        registry_type = var.image.registry_type
        registry      = var.image.registry
        repository    = var.image.repository
        digest        = var.image.digest

        deploy_on_push {
          enabled = false
        }
      }

      env {
        key   = "HEXROUTE_COMPONENT"
        value = "worker"
        scope = "RUN_TIME"
        type  = "GENERAL"
      }

      dynamic "env" {
        for_each = var.worker_environment
        content {
          key   = env.key
          value = env.value
          scope = "RUN_TIME"
          type  = "GENERAL"
        }
      }

      dynamic "env" {
        for_each = nonsensitive(toset(keys(var.worker_secret_environment)))
        content {
          key   = env.value
          value = var.worker_secret_environment[env.value]
          scope = "RUN_TIME"
          type  = "SECRET"
        }
      }

      termination {
        grace_period_seconds = 20
      }
    }

    ingress {
      rule {
        component {
          name = "api"
        }
        match {
          path {
            prefix = "/"
          }
        }
      }
    }
  }

  lifecycle {
    precondition {
      condition = (
        length(setintersection(local.api_keys, local.forbidden_management_keys)) == 0 &&
        length(setintersection(local.worker_keys, local.forbidden_management_keys)) == 0 &&
        length(setintersection(local.api_keys, local.forbidden_api_spaces_keys)) == 0
      )
      error_message = "provider management credentials cannot enter API or worker runtime, and Spaces runtime credentials are worker-only."
    }

    precondition {
      condition = (
        length(setintersection(local.api_keys, local.reserved_component_keys)) == 0 &&
        length(setintersection(local.worker_keys, local.reserved_component_keys)) == 0
      )
      error_message = "HEXROUTE_COMPONENT is reserved for module-owned App Platform dispatch."
    }

    precondition {
      condition = (
        length(setintersection(
          toset(keys(var.api_environment)),
          toset(nonsensitive(keys(var.api_secret_environment))),
        )) == 0 &&
        length(setintersection(
          toset(keys(var.worker_environment)),
          toset(nonsensitive(keys(var.worker_secret_environment))),
        )) == 0
      )
      error_message = "environment keys cannot be both GENERAL and SECRET."
    }
  }
}
