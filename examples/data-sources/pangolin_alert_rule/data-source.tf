data "pangolin_alert_rule" "example" {
  id = 12
}

output "alert_rule_recipients" {
  value = {
    users  = data.pangolin_alert_rule.example.user_ids
    roles  = data.pangolin_alert_rule.example.role_ids
    emails = data.pangolin_alert_rule.example.emails
  }
}
