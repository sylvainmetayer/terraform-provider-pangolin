# Every enabled alert rule whose name contains "offline".
data "pangolin_alert_rules" "offline" {
  query   = "offline"
  enabled = true
}

output "offline_rule_ids" {
  value = [for r in data.pangolin_alert_rules.offline.alert_rules : r.id]
}
