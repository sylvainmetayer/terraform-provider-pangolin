resource "pangolin_resource_pincode" "example" {
  resource_id = pangolin_resource.example.id
  pincode     = "123456"
}

# Write-only (Terraform >= 1.11, OpenTofu >= 1.11): the PIN code is never
# stored in the plan or the state. Change pincode_wo_version to send a new one.
resource "pangolin_resource_pincode" "write_only" {
  resource_id        = pangolin_resource.other.id
  pincode_wo         = var.resource_pincode
  pincode_wo_version = 1
}
