data "pangolin_ai_budgets" "all" {}

# Budgets that are scoped to a provider.
output "provider_budgets" {
  value = [for b in data.pangolin_ai_budgets.all.budgets : b.id if b.provider_id != null]
}
