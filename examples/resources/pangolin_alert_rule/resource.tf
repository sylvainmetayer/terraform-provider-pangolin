# Email the on-call role when any site goes offline.
resource "pangolin_alert_rule" "site_offline" {
  name             = "Site offline"
  event_type       = "site_offline"
  all_sites        = true
  cooldown_seconds = 300
  role_ids         = [pangolin_role.oncall.id]
  emails           = ["oncall@example.com"]
}

# Call a webhook with a bearer token when a resource becomes unhealthy.
resource "pangolin_alert_rule" "app_unhealthy" {
  name         = "App unhealthy"
  event_type   = "resource_unhealthy"
  resource_ids = [pangolin_resource.app.id]

  webhook_actions = [
    {
      url          = "https://hooks.example.com/pangolin"
      auth_type    = "bearer"
      bearer_token = var.alert_webhook_token
      headers = {
        "X-Source" = "pangolin"
      }
      body_template = jsonencode({
        text = "{{event}} at {{timestamp}}: {{data}}"
      })
    },
  ]
}
