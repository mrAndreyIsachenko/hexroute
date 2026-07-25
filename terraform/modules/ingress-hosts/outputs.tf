output "hosts" {
  value = {
    for name, host in terraform_data.host : name => host.output
  }
  description = "Validated provider-neutral ingress inventory."
}

output "independent_failure_domains" {
  value = (
    length(var.hosts) >= 2 &&
    length(local.providers) >= 2 &&
    length(local.asns) >= 2
  )
  description = "Whether the inventory spans at least two providers and ASNs."
}
