package resources

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func sessionRevalidationAttribute() schema.BoolAttribute {
	return schema.BoolAttribute{
		Description: "Check each Pangolin session against the identity provider with its refresh token, so a user disabled or removed there loses access without waiting for the session to expire. " +
			"Requires Pangolin 1.26.0 or later (an older server refuses the field) and an enterprise license. " +
			"Pangolin needs a refresh token: with `true`, `scopes` must contain `offline_access`, except for `variant = \"google\"`, where it must not (Google uses `access_type=offline`). " +
			"Null when the server does not report it.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Bool{
			boolplanmodifier.UseStateForUnknown(),
		},
	}
}

// validateSessionRevalidationScopes refuses a config the server would
// rewrite: with revalidation on, Pangolin adds offline_access to the
// scopes (removes it for Google), which would then drift from the config.
func validateSessionRevalidationScopes(enabled types.Bool, scopes, variant types.String) diag.Diagnostics {
	var diags diag.Diagnostics
	if enabled.IsUnknown() || !enabled.ValueBool() || scopes.IsUnknown() || variant.IsUnknown() {
		return diags
	}
	has := false
	for _, s := range strings.Fields(scopes.ValueString()) {
		if s == "offline_access" {
			has = true
		}
	}
	google := variant.ValueString() == "google"
	if google && has {
		diags.AddAttributeError(path.Root("scopes"), "offline_access not allowed with session revalidation on Google",
			"With session_revalidation = true and variant = \"google\", Pangolin removes offline_access from the scopes (Google uses access_type=offline). Remove it from scopes.")
	}
	if !google && !has {
		diags.AddAttributeError(path.Root("scopes"), "offline_access required for session revalidation",
			"With session_revalidation = true, Pangolin adds offline_access to the scopes to get a refresh token. Add it to scopes.")
	}
	return diags
}
