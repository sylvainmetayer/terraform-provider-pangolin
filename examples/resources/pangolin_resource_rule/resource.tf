# Allow only internal CIDR range
resource "pangolin_resource_rule" "allow_internal" {
  resource_id = pangolin_resource.example.id
  action      = "ACCEPT"
  match       = "CIDR"
  value       = "10.0.0.0/8"
  priority    = 1
  enabled     = true
}

# Block a specific country
resource "pangolin_resource_rule" "block_country" {
  resource_id = pangolin_resource.example.id
  action      = "DROP"
  match       = "COUNTRY"
  value       = "CN"
  priority    = 10
}

# Drop every request from outside France. DROP ends evaluation for the
# requests it matches; only French requests go on to the later rules.
resource "pangolin_resource_rule" "france_only" {
  resource_id = pangolin_resource.example.id
  action      = "DROP"
  match       = "COUNTRY_IS_NOT"
  value       = "FR"
  priority    = 20
}

# Drop write methods (comma-separated list)
resource "pangolin_resource_rule" "read_only" {
  resource_id = pangolin_resource.example.id
  action      = "DROP"
  match       = "METHOD"
  value       = "POST,PUT,PATCH,DELETE"
  priority    = 30
}
