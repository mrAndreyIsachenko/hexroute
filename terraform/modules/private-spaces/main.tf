resource "digitalocean_spaces_bucket" "this" {
  name          = var.name
  region        = var.region
  acl           = "private"
  force_destroy = false

  versioning {
    enabled = true
  }

  lifecycle_rule {
    id                                     = "bounded-private-retention"
    enabled                                = true
    abort_incomplete_multipart_upload_days = 7

    expiration {
      days = var.retention_days
    }

    noncurrent_version_expiration {
      days = var.retention_days
    }
  }
}

resource "digitalocean_spaces_key" "runtime" {
  count = var.create_runtime_key ? 1 : 0

  name = var.runtime_key_name

  grant {
    bucket     = digitalocean_spaces_bucket.this.name
    permission = "readwrite"
  }
}

resource "digitalocean_project_resources" "this" {
  count = var.project_id == null ? 0 : 1

  project   = var.project_id
  resources = [digitalocean_spaces_bucket.this.urn]
}
