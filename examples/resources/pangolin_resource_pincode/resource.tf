variable "resource_pincode" {
  type      = string
  sensitive = true
}

resource "pangolin_resource_pincode" "example" {
  resource_id = pangolin_resource.example.id
  pincode     = var.resource_pincode
}

# Write-only (Terraform >= 1.11, OpenTofu >= 1.11): the value is never stored
# in the plan or the state. An ephemeral variable (Terraform >= 1.10,
# OpenTofu >= 1.11) can feed a write-only argument, not the historical one.
# Changing the value alone sends nothing: bump pincode_wo_version to send it.
variable "resource_pincode_wo" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "pangolin_resource_pincode" "write_only" {
  resource_id        = pangolin_resource.other.id
  pincode_wo         = var.resource_pincode_wo
  pincode_wo_version = 1
}
