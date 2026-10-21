resource "pangolin_resource_policy_header_auth" "example" {
  resource_policy_id     = pangolin_resource_policy.example.id
  user                   = "admin"
  password               = var.header_auth_password
  extended_compatibility = false
}
