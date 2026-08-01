mock_provider "aws" {}

run "default_firewall" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
  }

  assert {
    condition     = aws_lightsail_instance.this.ip_address_type == "ipv4"
    error_message = "Lightsail ingress must use public IPv4 only."
  }

  assert {
    condition = (
      length(output.firewall_rules) == 1 &&
      output.firewall_rules[0].protocol == "tcp" &&
      output.firewall_rules[0].from_port == 443 &&
      output.firewall_rules[0].to_port == 443 &&
      toset(output.firewall_rules[0].cidrs) == toset(["0.0.0.0/0"])
    )
    error_message = "Default firewall must expose only global TCP 443."
  }
}

run "bounded_ssh" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [
      {
        protocol  = "tcp"
        from_port = 443
        to_port   = 443
        cidrs     = ["0.0.0.0/0"]
      },
      {
        protocol  = "tcp"
        from_port = 22
        to_port   = 22
        cidrs     = ["192.0.2.10/32"]
      },
    ]
  }

  assert {
    condition     = length(output.firewall_rules) == 2
    error_message = "One bounded SSH /32 must coexist with transport TCP 443."
  }
}

run "reject_public_ssh" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [
      {
        protocol  = "tcp"
        from_port = 443
        to_port   = 443
        cidrs     = ["0.0.0.0/0"]
      },
      {
        protocol  = "tcp"
        from_port = 22
        to_port   = 22
        cidrs     = ["0.0.0.0/0"]
      },
    ]
  }

  expect_failures = [var.public_ports]
}

run "reject_udp" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [{
      protocol  = "udp"
      from_port = 443
      to_port   = 443
      cidrs     = ["0.0.0.0/0"]
    }]
  }

  expect_failures = [var.public_ports]
}

run "reject_ipv6" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [{
      protocol  = "tcp"
      from_port = 443
      to_port   = 443
      cidrs     = ["::/0"]
    }]
  }

  expect_failures = [var.public_ports]
}

run "reject_port_range" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [{
      protocol  = "tcp"
      from_port = 443
      to_port   = 8443
      cidrs     = ["0.0.0.0/0"]
    }]
  }

  expect_failures = [var.public_ports]
}

run "reject_unreviewed_port" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    public_ports = [
      {
        protocol  = "tcp"
        from_port = 443
        to_port   = 443
        cidrs     = ["0.0.0.0/0"]
      },
      {
        protocol  = "tcp"
        from_port = 80
        to_port   = 80
        cidrs     = ["0.0.0.0/0"]
      },
    ]
  }

  expect_failures = [var.public_ports]
}
