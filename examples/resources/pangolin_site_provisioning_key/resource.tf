# A Newt started with `--provisioning-key` creates its own site.
resource "pangolin_site_provisioning_key" "homelab" {
  name              = "homelab-hosts"
  max_batch_size    = 5
  valid_until       = "2027-01-01T00:00:00Z"
  approve_new_sites = false
}

output "newt_provisioning_key" {
  value     = pangolin_site_provisioning_key.homelab.key
  sensitive = true
}
