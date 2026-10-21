data "pangolin_domains" "all" {}

# ---------------------------------------------------------------------
# HTTP resource - the historical Pangolin default. Reverse-proxied by
# the Pangolin edge and gated on a subdomain of a managed domain.
# ---------------------------------------------------------------------
resource "pangolin_resource" "web" {
  name      = "my-app"
  subdomain = "app"
  domain_id = data.pangolin_domains.all.domains[0].domain_id
  protocol  = "tcp"
  sso       = false # public access, no Pangolin authentication
}

# HTTP resource with 1.19+ options: injected headers, host-header
# override, PROXY-protocol upstream, post-auth landing page.
resource "pangolin_resource" "web_advanced" {
  name      = "internal-app"
  subdomain = "internal"
  domain_id = data.pangolin_domains.all.domains[0].domain_id
  mode      = "http"
  sso       = true

  set_host_header        = "backend.internal"
  enable_proxy           = true
  proxy_protocol         = true
  proxy_protocol_version = 2
  post_auth_path         = "/dashboard"

  headers = [
    { name = "X-Forwarded-User", value = "$user" },
    { name = "X-Env", value = "prod" },
  ]
}

# ---------------------------------------------------------------------
# Browser-based SSH (Pangolin >= 1.20). Reached through a domain like an
# HTTP resource; `pam_mode = "push"` pushes the user's credentials to
# the target host. Targets created for it inherit `mode = "ssh"`.
# ---------------------------------------------------------------------
resource "pangolin_resource" "ssh_bastion" {
  name             = "ssh-jumpbox"
  mode             = "ssh"
  subdomain        = "ssh"
  domain_id        = data.pangolin_domains.all.domains[0].domain_id
  pam_mode         = "push"
  auth_daemon_mode = "site"
  sso              = true
}

# ---------------------------------------------------------------------
# Browser-based RDP (Pangolin >= 1.20). `vnc` works the same way.
# ---------------------------------------------------------------------
resource "pangolin_resource" "rdp_desktop" {
  name      = "win-workstation"
  mode      = "rdp"
  subdomain = "desktop"
  domain_id = data.pangolin_domains.all.domains[0].domain_id
  sso       = true
}

# ---------------------------------------------------------------------
# Raw TCP resource on a public port of the Pangolin edge.
# ---------------------------------------------------------------------
resource "pangolin_resource" "postgres" {
  name       = "postgres"
  mode       = "tcp"
  proxy_port = 5432
}

# ---------------------------------------------------------------------
# AI gateway endpoint (Pangolin >= 1.22). The AI providers are created
# in Pangolin; their IDs are attached here in inherit mode.
# ---------------------------------------------------------------------
resource "pangolin_resource" "llm" {
  name            = "llm-gateway"
  mode            = "inference"
  subdomain       = "llm"
  domain_id       = data.pangolin_domains.all.domains[0].domain_id
  ai_provider_ids = [1, 2]
}

# ---------------------------------------------------------------------
# Maintenance-mode toggle. Any resource can be flipped into a
# maintenance page without touching the upstream config.
# ---------------------------------------------------------------------
resource "pangolin_resource" "under_maintenance" {
  name      = "docs"
  subdomain = "docs"
  domain_id = data.pangolin_domains.all.domains[0].domain_id
  mode      = "http"

  maintenance_mode_enabled   = true
  maintenance_mode_type      = "planned"
  maintenance_title          = "Docs are getting a facelift"
  maintenance_message        = "We'll be back in ~15 minutes."
  maintenance_estimated_time = "2026-07-05T14:00:00Z"
}
