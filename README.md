# Terraform Provider for Pangolin

The **Pangolin** Terraform provider lets you manage [Pangolin](https://github.com/fosrl/pangolin) infrastructure as code: organizations, sites, HTTP and private site resources, targets, roles, users, API keys, OLM clients, domains, identity providers, access-control rules, and all assignments.

This repository is a fork of [stackopshq/terraform-provider-pangolin](https://github.com/stackopshq/terraform-provider-pangolin), the original provider. The fork is not published on a registry yet: `stackopshq/pangolin` on the Terraform and OpenTofu registries is the upstream release (1.6.x), without the resources, data sources and fixes documented here.

[![License: MPL-2.0](https://img.shields.io/badge/License-MPL--2.0-blue)](LICENSE)

## Requirements

- [Terraform](https://www.terraform.io/) >= 1.0 or [OpenTofu](https://opentofu.org/) >= 1.6; the write-only attributes (`password_wo`, `pincode_wo`), which keep the secret out of the plan and the state, need Terraform >= 1.11 or OpenTofu >= 1.11
- A Pangolin self-hosted instance with the [Integration API](https://docs.pangolin.net/self-host/advanced/integration-api) enabled
- Go >= 1.25 (only for building from source, required by `terraform-plugin-framework`)

## Usage

```hcl
terraform {
  required_providers {
    pangolin = {
      source  = "stackopshq/pangolin"
      version = "1.7.0-sylvain.5"
    }
  }
}

provider "pangolin" {
  url     = "https://pangolin.example.com"
  api_key = var.pangolin_api_key
  org_id  = "my-org"
}
```

### Installing the fork

Until it is on a registry, the fork ships as pre-releases on [GitHub Releases](https://github.com/sylvainmetayer/terraform-provider-pangolin/releases): `linux_amd64` only and unsigned. Install one through an implicit local mirror, under the `stackopshq/pangolin` address the provider still uses:

```bash
version=1.7.0-sylvain.5
zip=terraform-provider-pangolin_${version}_linux_amd64.zip
base=https://github.com/sylvainmetayer/terraform-provider-pangolin/releases/download/v${version}
# registry.opentofu.org for OpenTofu, registry.terraform.io for Terraform
mirror=terraform.d/plugins/registry.opentofu.org/stackopshq/pangolin
mkdir -p "$mirror"
curl -fsSLo "$mirror/$zip" "$base/$zip"
curl -fsSL "$base/terraform-provider-pangolin_${version}_SHA256SUMS" | grep " $zip\$" | (cd "$mirror" && sha256sum --check)
```

Run this in the configuration directory, then `tofu init` (or `terraform init`). A pre-release is only selected by an exact `version`, as above.

All provider arguments can be set via environment variables:

```bash
export PANGOLIN_URL="https://pangolin.example.com"
export PANGOLIN_API_KEY="your-api-key"
export PANGOLIN_ORG_ID="your-org-id"
```

## Resources

| Resource | Description |
|----------|-------------|
| `pangolin_org` | Organization |
| `pangolin_site` | Site (Newt tunnel connector) |
| `pangolin_site_provisioning_key` | Key that lets Newt register new sites by itself |
| `pangolin_resource` | Public resource: HTTP reverse proxy, browser SSH / RDP / VNC, AI inference endpoint, or raw TCP / UDP |
| `pangolin_target` | Backend target for an HTTP resource |
| `pangolin_health_check` | Standalone health check run by a site (Enterprise) |
| `pangolin_site_resource` | Private site resource (TCP/UDP/SSH via VPN) |
| `pangolin_role` | Role |
| `pangolin_user` | User |
| `pangolin_domain` | Domain of the organization (wildcard, NS or CNAME) with the DNS records to create |
| `pangolin_redirect` | Redirect attached to a public resource or to a domain |
| `pangolin_label` | Label of the organization |
| `pangolin_label_attachment` | Attach a label to a site, a public or private resource, or a machine client |
| `pangolin_ai_provider` | AI provider (OpenAI, Anthropic, Gemini, Bedrock, custom...) served by `inference` resources |
| `pangolin_ai_model` | Allow or block one model of an AI provider |
| `pangolin_virtual_api_key` | Bearer key for the public inference resources of the AI gateway |
| `pangolin_ai_budget` | Spending cap of the AI gateway, per provider, model, resource, role, key or organization |
| `pangolin_event_streaming_destination` | Stream logs to an HTTP endpoint or an S3 bucket |
| `pangolin_custom_dns_record` | DNS record served by Pangolin's nameservers for a delegated (`ns`) domain |
| `pangolin_resource_mtls_certificate` | Trusted CA certificate for client-certificate (mTLS) authentication on a resource or a resource policy |
| `pangolin_api_key` | API key |
| `pangolin_api_key_actions` | Set of actions (permissions) granted to an API key |
| `pangolin_client` | OLM client device |
| `pangolin_client_role` | Role of a machine client (access to the role's private resources) |
| `pangolin_invitation` | Pending organization invitation |
| `pangolin_resource_role` | Assign a role to an HTTP resource |
| `pangolin_resource_user` | Assign a user to an HTTP resource |
| `pangolin_resource_whitelist` | Add an email to an HTTP resource whitelist |
| `pangolin_resource_rule` | Access-control rule for a resource (CIDR, IP, path, country, country exclusion, region, ASN, HTTP method) |
| `pangolin_resource_password` | Password protection for a resource (`password`, or write-only `password_wo`) |
| `pangolin_resource_pincode` | PIN-code protection for a resource (`pincode`, or write-only `pincode_wo`) |
| `pangolin_resource_header_auth` | Header-based authentication for a resource |
| `pangolin_resource_access_token` | Bearer access token bound to an HTTP resource |
| `pangolin_site_resource_role` | Assign a role to a private site resource |
| `pangolin_site_resource_user` | Assign a user to a private site resource |
| `pangolin_site_resource_client` | Assign an OLM client to a private site resource |
| `pangolin_role_user` | Assign a user to a role |
| `pangolin_user_role` | Additional (cumulative) role binding on a user |
| `pangolin_idp` | OIDC Identity Provider (global) |
| `pangolin_idp_org` | IDP-to-organization policy mapping |
| `pangolin_org_idp` | OIDC IDP scoped to a single organization (single-resource alternative to `pangolin_idp` + `pangolin_idp_org`) |
| `pangolin_alert_rule` | Alert rule: email and webhook notifications on site, health check or resource events (Enterprise) |
| `pangolin_resource_policy` | Shared resource policy: SSO, role / user grants, email whitelist, rules (Enterprise) |
| `pangolin_resource_policy_password` | Password protection for a resource policy (Enterprise) |
| `pangolin_resource_policy_pincode` | PIN-code protection for a resource policy (Enterprise) |
| `pangolin_resource_policy_header_auth` | Header-based authentication for a resource policy (Enterprise) |

## Data Sources

| Data Source | Description |
|-------------|-------------|
| `pangolin_domains` | List all domains |
| `pangolin_domain` | Single domain lookup by ID |
| `pangolin_domain_dns_records` | DNS records expected (or verified) for a domain |
| `pangolin_sites` | List all sites |
| `pangolin_site` | Single site lookup by nice ID (full live payload) |
| `pangolin_resources` | List all HTTP resources |
| `pangolin_resource_targets` | List backend targets of a resource (with health-check + routing detail) |
| `pangolin_health_checks` | List standalone and target-attached health checks (Enterprise) |
| `pangolin_health_check_status_history` | Daily uptime history of a health check (Enterprise) |
| `pangolin_resource_roles` | List roles granted access to a resource |
| `pangolin_site_resources` | List all private site resources |
| `pangolin_roles` | List all roles |
| `pangolin_users` | List all users |
| `pangolin_user` | Single user lookup by username + idp_id |
| `pangolin_user_by_id` | Single user lookup by user_id (root-only; exposes `server_admin`, `email_verified`, `two_factor_setup_requested`) |
| `pangolin_user_devices` | List user-bound devices for an org (filter by agent, status, online, query; sort by bandwidth) |
| `pangolin_api_keys` | List all API keys |
| `pangolin_access_tokens` | List all resource access tokens in the organization |
| `pangolin_orgs` | List all organizations visible to the calling key (root-only) |
| `pangolin_idps` | List all Identity Providers |
| `pangolin_request_logs` | Query the request audit log |
| `pangolin_access_logs` | Query the access audit log (per-request access decisions) |
| `pangolin_action_logs` | Query the action audit log (admin/mutation actions) |
| `pangolin_connection_logs` | Query the connection audit log (VPN/tunnel lifecycle events) |
| `pangolin_alert_rules` | List alert rules (filter by name and enabled state) (Enterprise) |
| `pangolin_alert_rule` | Single alert rule lookup by ID (with recipients and webhook actions) (Enterprise) |
| `pangolin_logs_analytics` | Aggregate analytics rollup (per-country, per-day, totals) |
| `pangolin_blueprints` | List the blueprint (declarative apply) audit records for the org |
| `pangolin_blueprint` | Single blueprint lookup by ID (exposes raw `contents` + apply outcome) |
| `pangolin_resource_policies` | List the shared resource policies of the org with their attached resources (Enterprise) |
| `pangolin_resource_policy` | Single resource policy lookup by ID (grants, whitelist, rules, which secrets are set) (Enterprise) |
| `pangolin_ai_providers` | List the AI gateway providers (no API key, no headers) |
| `pangolin_ai_models` | List the model entries of an AI provider |
| `pangolin_ai_catalog_models` | List the model keys Pangolin's catalog knows for a provider type |
| `pangolin_ai_budgets` | List the AI gateway budgets |
| `pangolin_virtual_api_keys` | List the manual virtual API keys (never the secret) |
| `pangolin_redirects` | List redirects (filter by text, type, enabled state, resource and domain) |

## Example

```hcl
# Create a site (Newt tunnel connector)
resource "pangolin_site" "homelab" {
  name = "homelab"
}

# Expose an app publicly via HTTP reverse proxy
data "pangolin_domains" "all" {}

resource "pangolin_resource" "app" {
  name      = "my-app"
  subdomain = "app"
  domain_id = data.pangolin_domains.all.domains[0].domain_id
  mode      = "http"
}

resource "pangolin_target" "app" {
  resource_id = pangolin_resource.app.id
  site_id     = pangolin_site.homelab.id
  ip          = "localhost"
  port        = 8080
}

# Restrict access to a role
resource "pangolin_role" "devs" {
  name = "developers"
}

resource "pangolin_resource_role" "app_devs" {
  resource_id = pangolin_resource.app.id
  role_id     = pangolin_role.devs.id
}

# Expose a TCP service via the VPN (private site resource)
resource "pangolin_site_resource" "postgres" {
  site_ids       = [pangolin_site.homelab.id]
  name           = "postgres"
  mode           = "host"
  destination    = "db.internal"
  alias          = "db.local"
  tcp_port_range = "5432"
}

# SSH bastion via PAM (Pangolin 1.19+)
resource "pangolin_site_resource" "ssh_bastion" {
  site_ids    = [pangolin_site.homelab.id]
  name        = "ssh-bastion"
  mode        = "ssh"
  destination = "bastion.internal"
  alias       = "bastion.local"
  pam_mode    = "push"
}
```

Full documentation is in [`docs/`](docs). The [Terraform Registry](https://registry.terraform.io/providers/stackopshq/pangolin/latest/docs) only documents the upstream release.

## Development

```bash
# Build
go build -o terraform-provider-pangolin

# Run tests
go test ./...

# Regenerate docs (idempotent)
go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name pangolin

# Install locally for testing
mkdir -p ~/.terraform.d/plugins/registry.terraform.io/stackopshq/pangolin/1.5.0/$(go env GOOS)_$(go env GOARCH)
cp terraform-provider-pangolin ~/.terraform.d/plugins/registry.terraform.io/stackopshq/pangolin/1.5.0/$(go env GOOS)_$(go env GOARCH)/
```

## License

[Mozilla Public License 2.0](LICENSE)
