# HTTP probe run by the homelab site's Newt connector.
resource "pangolin_health_check" "api" {
  name    = "api"
  site_id = pangolin_site.homelab.id
  enabled = true

  mode     = "http"
  scheme   = "https"
  hostname = "api.internal"
  port     = 8443
  path     = "/healthz"
  method   = "GET"
  status   = 200

  interval            = 30
  unhealthy_interval  = 10
  timeout             = 5
  healthy_threshold   = 2
  unhealthy_threshold = 3

  headers = [
    { name = "X-Probe", value = "pangolin" },
  ]
}

# Plain TCP connect check.
resource "pangolin_health_check" "postgres" {
  name     = "postgres"
  site_id  = pangolin_site.homelab.id
  enabled  = true
  mode     = "tcp"
  hostname = "db.internal"
  port     = 5432
}
