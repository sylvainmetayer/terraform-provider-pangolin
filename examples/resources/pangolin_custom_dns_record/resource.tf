# A domain delegated to Pangolin's nameservers (Pangolin 1.26.0+, enterprise).
resource "pangolin_domain" "zone" {
  base_domain = "example.org"
  type        = "ns"
}

# Names are relative to the domain ("@" for the apex), lowercase, and
# hostnames have no trailing dot: the form Pangolin stores.
resource "pangolin_custom_dns_record" "mx" {
  domain_id = pangolin_domain.zone.id
  name      = "@"
  type      = "MX"
  values    = ["mail.example.net"]
  priority  = 10
}

resource "pangolin_custom_dns_record" "spf" {
  domain_id = pangolin_domain.zone.id
  name      = "@"
  type      = "TXT"
  values    = ["v=spf1 mx -all"] # no surrounding quotes
  ttl       = 3600
}

# Several values go into one record: one record per name and type.
resource "pangolin_custom_dns_record" "delegation" {
  domain_id = pangolin_domain.zone.id
  name      = "lab"
  type      = "NS"
  values    = ["ns1.example.net", "ns2.example.net"]
}
