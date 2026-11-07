# Keys that may call one public resource. The secret is never listed.
data "pangolin_virtual_api_keys" "for_resource" {
  resource_id = pangolin_resource.llm.id
}

output "key_names" {
  value = [for k in data.pangolin_virtual_api_keys.for_resource.virtual_api_keys : k.name]
}
