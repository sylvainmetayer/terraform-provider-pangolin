package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// secretValue returns the historical attribute when it is set, otherwise
// the write-only one, which only the configuration holds (plan and state
// always carry null for it).
func secretValue(ctx context.Context, cfg tfsdk.Config, plain types.String, writeOnly path.Path) (string, diag.Diagnostics) {
	if !plain.IsNull() {
		return plain.ValueString(), nil
	}
	var v types.String
	diags := cfg.GetAttribute(ctx, writeOnly, &v)
	return v.ValueString(), diags
}
