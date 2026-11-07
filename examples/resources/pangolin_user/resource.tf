# OIDC user, signing in through an identity provider linked to the org.
resource "pangolin_user" "alice" {
  username = "alice"
  email    = "alice@example.com"
  name     = "Alice"
  idp_id   = pangolin_idp.authentik.id
  role_id  = pangolin_role.developers.id

  # The IdP must be linked to the org before users can be added to it.
  depends_on = [pangolin_idp_org.authentik]
}
