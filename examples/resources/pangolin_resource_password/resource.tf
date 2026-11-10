resource "pangolin_resource_password" "example" {
  resource_id = pangolin_resource.example.id
  password    = var.resource_password
}

# Write-only (Terraform >= 1.11, OpenTofu >= 1.11): the password is never
# stored in the plan or the state. Change password_wo_version to send a new one.
resource "pangolin_resource_password" "write_only" {
  resource_id         = pangolin_resource.other.id
  password_wo         = var.resource_password
  password_wo_version = 1
}
