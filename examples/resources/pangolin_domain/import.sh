# Domains import by domain ID. Look it up with the pangolin_domains
# data source or in the Pangolin web UI.
#
# Note: a domain declared in the Pangolin config file (config_managed
# = true) cannot be deleted through the API; `terraform destroy` then
# fails until it is removed from the config file or from the state.
terraform import pangolin_domain.example <domain_id>
