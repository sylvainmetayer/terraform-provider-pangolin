# Last 30 days of uptime, with day boundaries at UTC+2.
data "pangolin_health_check_status_history" "api" {
  health_check_id   = pangolin_health_check.api.id
  days              = 30
  tz_offset_minutes = 120
}

output "api_uptime_percent" {
  value = data.pangolin_health_check_status_history.api.overall_uptime_percent
}

output "api_bad_days" {
  value = [
    for d in data.pangolin_health_check_status_history.api.history : d.date if d.status == "bad"
  ]
}
