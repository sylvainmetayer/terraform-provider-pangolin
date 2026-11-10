# Standalone health checks import by numeric ID. Look it up via the
# pangolin_health_checks data source. The probe of an AI provider target
# (pangolin_target with ai_provider_id) is listed as standalone too:
# do not import it, pangolin_target already manages it.
terraform import pangolin_health_check.example <health_check_id>
