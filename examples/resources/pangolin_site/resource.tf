resource "pangolin_site" "example" {
  name = "paris-01"
}

# Overrides the organization's newt_auto_update for this site only
# (enterprise). Leave it unset to follow the organization.
resource "pangolin_site" "pinned" {
  name                  = "lab-01"
  newt_auto_update      = false
  docker_socket_enabled = false
}

output "example_newt_id" {
  value = pangolin_site.example.newt_id
}

output "example_newt_secret" {
  value     = pangolin_site.example.newt_secret
  sensitive = true
}
