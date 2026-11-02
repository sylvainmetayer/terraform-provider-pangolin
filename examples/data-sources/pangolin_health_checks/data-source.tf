data "pangolin_health_checks" "all" {}

# Standalone checks that are currently failing.
data "pangolin_health_checks" "unhealthy" {
  health  = "unhealthy"
  enabled = true
}

output "unhealthy_standalone_checks" {
  value = [
    for hc in data.pangolin_health_checks.unhealthy.health_checks : hc.name if hc.standalone
  ]
}
