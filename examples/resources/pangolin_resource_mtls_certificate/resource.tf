# Clients must present a certificate signed by this CA to reach the resource.
# enable_mtls turns the check on after the certificate is stored.
resource "pangolin_resource_mtls_certificate" "ca" {
  resource_id = pangolin_resource.app.id
  name        = "internal CA"
  certificate = file("${path.module}/ca.pem")
  enable_mtls = true
}

# The same on a shared resource policy.
resource "pangolin_resource_mtls_certificate" "policy_ca" {
  resource_policy_id = pangolin_resource_policy.shared.id
  certificate        = file("${path.module}/ca.pem")
  enable_mtls        = true
}
