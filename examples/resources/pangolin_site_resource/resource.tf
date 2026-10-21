# L4 tunnel (cidr / host modes) - requires alias + port ranges.
resource "pangolin_site_resource" "internal_db" {
  site_ids       = [pangolin_site.example.id]
  name           = "internal-db"
  mode           = "host"
  destination    = "db.internal"
  alias          = "db.local"
  tcp_port_range = "5432"
}

# Same service reachable through two sites (failover).
resource "pangolin_site_resource" "replicated_db" {
  site_ids       = [pangolin_site.example.id, pangolin_site.backup.id]
  name           = "replicated-db"
  mode           = "cidr"
  destination    = "10.20.0.0/24"
  alias          = "replica.local"
  tcp_port_range = "5432"
}

# L7 HTTP proxy (http mode) - requires domain_id + subdomain + scheme + destination_port.
resource "pangolin_site_resource" "app_proxy" {
  site_ids         = [pangolin_site.example.id]
  name             = "app-proxy"
  mode             = "http"
  destination      = "10.0.0.42"
  domain_id        = data.pangolin_domains.all.domains[0].id
  subdomain        = "app"
  scheme           = "http"
  destination_port = 8080
  ssl              = true
}

output "app_full_domain" {
  value = pangolin_site_resource.app_proxy.full_domain
}

# SSH server (ssh mode) with PAM push notification MFA. The server
# forces tcp_port_range to destination_port ("22" when unset), so leave
# the port ranges and disable_icmp unset. A hostname destination needs
# an alias.
resource "pangolin_site_resource" "ssh_bastion" {
  site_ids         = [pangolin_site.example.id]
  name             = "ssh-bastion"
  mode             = "ssh"
  destination      = "bastion.internal"
  alias            = "bastion.local"
  destination_port = 2222
  pam_mode         = "push"
}

# Exit node (gateway mode, Pangolin 1.24+) - routes all client traffic
# through the site. The server forces destination = "0.0.0.0/0", both
# port ranges to "*" and disable_icmp = false, so leave them unset.
resource "pangolin_site_resource" "exit_node" {
  site_ids = [pangolin_site.example.id]
  name     = "exit-node"
  mode     = "gateway"
}

# AI inference endpoint (inference mode, Pangolin 1.22+) - served through
# the exit node, so it has no site. The AI providers are created outside
# Terraform for now; reference them by ID.
resource "pangolin_site_resource" "llm" {
  name            = "llm"
  mode            = "inference"
  domain_id       = data.pangolin_domains.all.domains[0].id
  subdomain       = "llm"
  ssl             = true
  ai_provider_ids = [3]
}

# A resource can be kept but disabled.
resource "pangolin_site_resource" "staging_db" {
  site_ids    = [pangolin_site.example.id]
  name        = "staging-db"
  mode        = "host"
  destination = "10.0.0.7"
  alias       = "staging-db.local"
  enabled     = false
}
