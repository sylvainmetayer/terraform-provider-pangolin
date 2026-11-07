package resources

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSameInstant(t *testing.T) {
	srv := "2027-01-01T00:00:00.000Z"
	if got := sameInstant(types.StringValue("2027-01-01T01:00:00+01:00"), &srv); got.ValueString() != "2027-01-01T01:00:00+01:00" {
		t.Errorf("same instant must keep the configured form, got %s", got)
	}
	if got := sameInstant(types.StringValue("2028-01-01T00:00:00Z"), &srv); got.ValueString() != srv {
		t.Errorf("a different instant must take the server value, got %s", got)
	}
	if got := sameInstant(types.StringValue("2028-01-01T00:00:00Z"), nil); !got.IsNull() {
		t.Errorf("no expiry on the server must be null, got %s", got)
	}
}
