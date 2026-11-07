resource "pangolin_role" "ci" {
  name = "ci"
}

resource "pangolin_client" "ci_runner" {
  name = "ci-runner"
}

# The CI runner reaches every private resource given to the "ci" role.
resource "pangolin_client_role" "ci_runner" {
  client_id = pangolin_client.ci_runner.id
  role_id   = pangolin_role.ci.id
}
