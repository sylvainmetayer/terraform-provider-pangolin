# Every enabled permanent redirect, with the resource it is attached to.
data "pangolin_redirects" "permanent" {
  type    = "permanent"
  enabled = true
}

output "permanent_redirects" {
  value = { for r in data.pangolin_redirects.permanent.redirects : r.name => r.destination_host }
}
