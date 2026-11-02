# Wildcard domain: point *.apps.example.com and apps.example.com at the
# Pangolin server. Usable at once; the only type Community Edition
# accepts.
resource "pangolin_domain" "apps" {
  base_domain          = "apps.example.com"
  type                 = "wildcard"
  cert_resolver        = "letsencrypt"
  prefer_wildcard_cert = true
}

# CNAME domain (Enterprise Edition / Pangolin Cloud): create the
# records Pangolin expects at your DNS provider, then wait for
# Pangolin to set `verified`.
resource "pangolin_domain" "portal" {
  base_domain = "portal.example.com"
  type        = "cname"
}

output "portal_dns_records" {
  value = pangolin_domain.portal.dns_records
}
