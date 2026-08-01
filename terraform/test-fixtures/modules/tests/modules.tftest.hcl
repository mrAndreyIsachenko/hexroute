mock_provider "digitalocean" {}
mock_provider "uptimerobot" {}
mock_provider "aws" {}

run "synthetic_composition" {
  command = plan

  assert {
    condition     = length(module.uptime_checks.check_ids) == 5
    error_message = "synthetic monitoring must cover every supported black-box category."
  }

  assert {
    condition     = module.ingress_hosts.independent_failure_domains
    error_message = "synthetic ingress inventory must span independent failure domains."
  }

  assert {
    condition = (
      length(module.lightsail_ingress.firewall_rules) == 1 &&
      module.lightsail_ingress.firewall_rules[0].protocol == "tcp" &&
      module.lightsail_ingress.firewall_rules[0].from_port == 443 &&
      module.lightsail_ingress.firewall_rules[0].to_port == 443 &&
      toset(module.lightsail_ingress.firewall_rules[0].cidrs) ==
      toset(["0.0.0.0/0"])
    )
    error_message = "synthetic Lightsail ingress must expose only global TCP 443."
  }
}
