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

  assert {
    condition = (
      aws_lightsail_instance.this.user_data == null &&
      output.runtime_bootstrap_sha256 == null &&
      output.runtime_artifact_versions == null
    )
    error_message = "Runtime bootstrap must remain absent until exact artifacts are supplied."
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

run "runtime_bootstrap" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    runtime_artifacts = {
      xray = {
        version = "25.7.1"
        url     = "https://downloads.example.invalid/xray-25.7.1-linux-amd64.zip"
        sha256  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
      observer = {
        version = "1.0.0"
        url     = "https://downloads.example.invalid/observer-1.0.0-linux-amd64.tar.gz"
        sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }

  assert {
    condition = (
      aws_lightsail_instance.this.user_data != null &&
      strcontains(aws_lightsail_instance.this.user_data, "hexroute-xray.service") &&
      strcontains(aws_lightsail_instance.this.user_data, "hexroute-ingress-observer.service") &&
      length(output.runtime_bootstrap_sha256) == 64
    )
    error_message = "Pinned artifacts must render deterministic service bootstrap."
  }

  assert {
    condition = (
      output.runtime_artifact_versions.xray == "25.7.1" &&
      output.runtime_artifact_versions.observer == "1.0.0"
    )
    error_message = "Only exact non-secret artifact versions may be exposed."
  }
}

run "reject_floating_version" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    runtime_artifacts = {
      xray = {
        version = "latest"
        url     = "https://downloads.example.invalid/xray.zip"
        sha256  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
      observer = {
        version = "1.0.0"
        url     = "https://downloads.example.invalid/observer.tar.gz"
        sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }

  expect_failures = [var.runtime_artifacts]
}

run "reject_credentialed_url" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    runtime_artifacts = {
      xray = {
        version = "25.7.1"
        url     = "https://downloads.example.invalid/xray.zip?signature=canary"
        sha256  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
      observer = {
        version = "1.0.0"
        url     = "https://downloads.example.invalid/observer.tar.gz"
        sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }

  expect_failures = [var.runtime_artifacts]
}

run "reject_malformed_digest" {
  command = plan

  variables {
    name              = "hexroute-example-ingress"
    availability_zone = "us-east-1a"
    blueprint_id      = "ubuntu_24_04"
    bundle_id         = "micro_3_0"
    runtime_artifacts = {
      xray = {
        version = "25.7.1"
        url     = "https://downloads.example.invalid/xray.zip"
        sha256  = "not-a-sha256"
      }
      observer = {
        version = "1.0.0"
        url     = "https://downloads.example.invalid/observer.tar.gz"
        sha256  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }

  expect_failures = [var.runtime_artifacts]
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
