variable "resource_password" {
  type      = string
  sensitive = true
}

resource "pangolin_resource_password" "example" {
  resource_id = pangolin_resource.example.id
  password    = var.resource_password
}

# Write-only (Terraform >= 1.11, OpenTofu >= 1.11): the value is never stored
# in the plan or the state. An ephemeral variable (Terraform >= 1.10,
# OpenTofu >= 1.11) can feed a write-only argument, not the historical one.
# Changing the value alone sends nothing: bump password_wo_version to send it.
variable "resource_password_wo" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "pangolin_resource_password" "write_only" {
  resource_id         = pangolin_resource.other.id
  password_wo         = var.resource_password_wo
  password_wo_version = 1
}
