mock_provider "digitalocean" {}
mock_provider "uptimerobot" {}

run "synthetic_composition" {
  command = plan

  assert {
    condition     = module.ingress_hosts.independent_failure_domains
    error_message = "synthetic ingress inventory must span independent failure domains."
  }
}
