# Import a CA certificate as resource/{resource_id}/{id} or policy/{resource_policy_id}/{id}.
# Pangolin never returns the PEM: set `certificate` in the configuration afterwards.
terraform import pangolin_resource_mtls_certificate.ca resource/<resource_id>/<certificate_id>
