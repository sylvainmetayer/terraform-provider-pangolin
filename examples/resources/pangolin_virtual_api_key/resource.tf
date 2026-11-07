# A key limited to one public inference resource, valid for 30 days.
resource "pangolin_virtual_api_key" "ci" {
  name              = "ci"
  description       = "Used by the CI runners"
  resource_ids      = [pangolin_resource.llm.id]
  valid_for_seconds = 2592000
}

# A key for every public inference resource.
resource "pangolin_virtual_api_key" "all" {
  name          = "all"
  all_resources = true
}

output "ci_key" {
  value     = pangolin_virtual_api_key.ci.secret
  sensitive = true
}
