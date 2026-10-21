data "pangolin_domains" "all" {}

# Shared access settings for every internal tool (Pangolin Enterprise).
resource "pangolin_resource_policy" "internal" {
  name     = "internal-tools"
  sso      = true
  role_ids = [pangolin_role.devs.id]
  user_ids = [pangolin_user.alice.id]

  email_whitelist_enabled = true
  whitelist_emails        = ["*@example.com"]

  apply_rules = true
  rules = [
    { action = "ACCEPT", match = "CIDR", value = "10.0.0.0/8", priority = 1 },
    { action = "DROP", match = "COUNTRY_IS_NOT", value = "FR", priority = 2 },
    { action = "DROP", match = "METHOD", value = "DELETE", priority = 3, enabled = false },
  ]
}

# Attach resources to the policy. Set `resource_policy_id = 0` to
# detach a resource and fall back to its own inline policy; leaving the
# attribute unset keeps whatever attachment the resource already has.
resource "pangolin_resource" "wiki" {
  name               = "wiki"
  subdomain          = "wiki"
  domain_id          = data.pangolin_domains.all.domains[0].domain_id
  resource_policy_id = pangolin_resource_policy.internal.id
}
