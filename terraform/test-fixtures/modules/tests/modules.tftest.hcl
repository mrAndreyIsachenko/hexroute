mock_provider "digitalocean" {}
mock_provider "uptimerobot" {}

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
}
