# Import format: "{org_id}/{idp_id}"
#
# Note: Pangolin returns `client_secret` decrypted, so the import reads
# it back into the Terraform state. Protect the state accordingly.
terraform import pangolin_org_idp.authentik my-org/3
