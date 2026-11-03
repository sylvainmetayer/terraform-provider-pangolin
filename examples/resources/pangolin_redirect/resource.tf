# Whole-domain redirect: old.example.com/anything to https://new.example.com/anything.
resource "pangolin_redirect" "old_site" {
  name             = "old-site"
  domain_id        = data.pangolin_domains.all.domains[0].domain_id
  subdomain        = "old"
  destination_host = "https://new.example.com"
  permanent        = true
}

# Redirect on a public resource, stripping the matched prefix.
resource "pangolin_redirect" "docs" {
  name              = "docs"
  resource_id       = pangolin_resource.app.id
  destination_host  = "https://docs.example.com"
  path_match_type   = "prefix"
  match_path        = "/docs"
  rewrite_path_type = "stripPrefix"
}
