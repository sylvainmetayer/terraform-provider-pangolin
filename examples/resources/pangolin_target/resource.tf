resource "pangolin_target" "example" {
  resource_id = pangolin_resource.example.id
  site_id     = pangolin_site.example.id
  ip          = "192.168.1.10"
  port        = 8080
  method      = "http"
}

# Target with an active HTTP health check.
resource "pangolin_target" "healthchecked" {
  resource_id = pangolin_resource.example.id
  site_id     = pangolin_site.example.id
  ip          = "192.168.1.11"
  port        = 8080
  method      = "http"

  hc_enabled             = true
  hc_hostname            = "192.168.1.11"
  hc_path                = "/health"
  hc_method              = "GET"
  hc_status              = 200
  hc_interval            = 30
  hc_unhealthy_interval  = 10
  hc_timeout             = 5
  hc_healthy_threshold   = 2
  hc_unhealthy_threshold = 3

  hc_headers = [
    { name = "X-Probe", value = "pangolin" },
  ]
}

# Target of a custom AI provider in target routing mode: the AI gateway
# reaches a self-hosted model server behind the site's tunnel.
resource "pangolin_ai_provider" "ollama" {
  name         = "ollama"
  type         = "custom"
  routing_mode = "target"
  auth_type    = "none"
  capabilities = ["openai_chat", "v1_models"]
}

resource "pangolin_target" "ollama" {
  ai_provider_id = pangolin_ai_provider.ollama.id
  site_id        = pangolin_site.example.id
  ip             = "192.168.1.20"
  port           = 11434
  method         = "http"
}
