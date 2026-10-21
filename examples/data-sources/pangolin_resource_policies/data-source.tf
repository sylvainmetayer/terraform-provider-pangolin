# Shared resource policies of the org, filtered on name / nice ID.
data "pangolin_resource_policies" "internal" {
  query = "internal"
}

# Map each policy name to the resources attached to it.
output "policy_resources" {
  value = {
    for p in data.pangolin_resource_policies.internal.policies :
    p.name => [for r in p.resources : r.name]
  }
}
