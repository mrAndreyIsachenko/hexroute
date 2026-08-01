output "instance_name" {
  value       = aws_lightsail_instance.this.name
  description = "Lightsail instance name."
}

output "instance_arn" {
  value       = aws_lightsail_instance.this.arn
  description = "Computed Lightsail instance ARN."
}

output "static_ip_name" {
  value       = aws_lightsail_static_ip.this.name
  description = "Lightsail static IPv4 allocation name."
}

output "public_ip_address" {
  value       = aws_lightsail_static_ip.this.ip_address
  description = "Computed attached static public IPv4 address."
}

output "private_ip_address" {
  value       = aws_lightsail_instance.this.private_ip_address
  description = "Computed Lightsail private IPv4 address."
}

output "username" {
  value       = aws_lightsail_instance.this.username
  description = "Computed operating-system login name for the selected blueprint."
}

output "firewall_rules" {
  value       = local.normalized_public_ports
  description = "Normalized authoritative IPv4 public-port policy."
}

output "runtime_bootstrap_sha256" {
  value       = local.runtime_bootstrap == null ? null : sha256(local.runtime_bootstrap)
  description = "Digest of rendered non-secret cloud-init; null when runtime bootstrap is disabled."
}

output "runtime_artifact_versions" {
  value = var.runtime_artifacts == null ? null : {
    xray     = var.runtime_artifacts.xray.version
    observer = var.runtime_artifacts.observer.version
  }
  description = "Exact non-secret runtime artifact versions."
}
