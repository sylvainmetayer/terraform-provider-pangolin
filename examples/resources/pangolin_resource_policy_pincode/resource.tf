resource "pangolin_resource_policy_pincode" "example" {
  resource_policy_id = pangolin_resource_policy.example.id
  pincode            = var.policy_pincode # exactly 6 digits
}
