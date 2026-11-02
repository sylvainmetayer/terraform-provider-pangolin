resource "pangolin_resource_policy_password" "example" {
  resource_policy_id = pangolin_resource_policy.example.id
  password           = var.policy_password
}
