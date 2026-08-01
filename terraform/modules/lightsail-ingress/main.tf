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
}

resource "aws_lightsail_instance" "this" {
  name              = var.name
  availability_zone = var.availability_zone
  blueprint_id      = var.blueprint_id
  bundle_id         = var.bundle_id
  key_pair_name     = var.key_pair_name
  ip_address_type   = "ipv4"
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
