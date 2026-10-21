# Single resource policy lookup by ID.
data "pangolin_resource_policy" "internal" {
  id = 7
}

output "policy_is_password_protected" {
  value = data.pangolin_resource_policy.internal.has_password
}
