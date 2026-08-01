locals {
  tags = merge(var.tags, {
    HexrouteManaged   = "true"
    HexrouteComponent = "lightsail-ingress"
  })

  normalized_public_ports = [
    for rule in var.public_ports : {
      protocol  = rule.protocol
      from_port = rule.from_port
      to_port   = rule.to_port
      cidrs     = sort(tolist(rule.cidrs))
    }
  ]

  xray_service = templatefile("${path.module}/templates/hexroute-xray.service.tftpl", {})
  observer_service = templatefile(
    "${path.module}/templates/hexroute-ingress-observer.service.tftpl",
    {},
  )
  runtime_installer = var.runtime_artifacts == null ? null : templatefile(
    "${path.module}/templates/install-runtime.sh.tftpl",
    {
      xray_version     = var.runtime_artifacts.xray.version
      xray_url         = var.runtime_artifacts.xray.url
      xray_sha256      = var.runtime_artifacts.xray.sha256
      observer_version = var.runtime_artifacts.observer.version
      observer_url     = var.runtime_artifacts.observer.url
      observer_sha256  = var.runtime_artifacts.observer.sha256
    },
  )
  runtime_bootstrap = var.runtime_artifacts == null ? null : templatefile(
    "${path.module}/templates/cloud-init.yaml.tftpl",
    {
      xray_service      = local.xray_service
      observer_service  = local.observer_service
      runtime_installer = local.runtime_installer
    },
  )
}

resource "aws_lightsail_instance" "this" {
  name              = var.name
  availability_zone = var.availability_zone
  blueprint_id      = var.blueprint_id
  bundle_id         = var.bundle_id
  key_pair_name     = var.key_pair_name
  ip_address_type   = "ipv4"
  user_data         = local.runtime_bootstrap
  tags              = local.tags

  add_on {
    type          = "AutoSnapshot"
    snapshot_time = var.automatic_snapshot_time_utc
    status        = var.automatic_snapshot_enabled ? "Enabled" : "Disabled"
  }
}

resource "aws_lightsail_static_ip" "this" {
  name = "${var.name}-ipv4"
}

resource "aws_lightsail_static_ip_attachment" "this" {
  instance_name  = aws_lightsail_instance.this.name
  static_ip_name = aws_lightsail_static_ip.this.name
}

resource "aws_lightsail_instance_public_ports" "this" {
  instance_name = aws_lightsail_instance.this.name

  dynamic "port_info" {
    for_each = local.normalized_public_ports

    content {
      protocol  = port_info.value.protocol
      from_port = port_info.value.from_port
      to_port   = port_info.value.to_port
      cidrs     = port_info.value.cidrs
    }
  }
}
