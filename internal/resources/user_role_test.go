package resources

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/stackopshq/terraform-provider-pangolin/internal/client"
)

func TestRemoveRoleDiags(t *testing.T) {
	cases := []struct {
		name          string
		err           error
		wantErr, warn bool
	}{
		{"ok", nil, false, false},
		{"gone", fmt.Errorf("x: %w", client.ErrNotFound), false, false},
		{"last role", errors.New("API error (status 403): User must have at least one role in the organization."), false, true},
		{"other", errors.New("boom"), true, false},
	}
	for _, c := range cases {
		var d diag.Diagnostics
		removeRoleDiags(c.err, &d)
		if d.HasError() != c.wantErr || (d.WarningsCount() > 0) != c.warn {
			t.Errorf("%s: diags = %v", c.name, d)
		}
	}
}
