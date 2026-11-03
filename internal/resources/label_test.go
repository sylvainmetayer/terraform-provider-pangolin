package resources

import (
	"fmt"
	"testing"
)

func TestLabelColorPattern(t *testing.T) {
	for color, want := range map[string]bool{"#ff0000": true, "FF0000": true, "#abc": true, "abcd": false, "#ggg": false, "": false} {
		if got := labelColorPattern.MatchString(color); got != want {
			t.Errorf("color %q: match = %v, want %v", color, got, want)
		}
	}
}

func TestParseLabelAttachmentID_RoundTrip(t *testing.T) {
	for _, id := range []string{"4/site/1", "4/resource/2", "4/site_resource/3", "4/client/5"} {
		m, err := parseLabelAttachmentID(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		_, kind, target := labelAttachmentTarget(m)
		if got := m.LabelID.ValueInt64(); got != 4 {
			t.Errorf("%s: label = %d", id, got)
		}
		if id != fmt.Sprintf("4/%s/%d", kind, target) {
			t.Errorf("%s: kind %s, target %d", id, kind, target)
		}
	}
	for _, id := range []string{"", "4", "4/site", "x/site/1", "4/site/x", "4/user/1", "4/site/1/2"} {
		if _, err := parseLabelAttachmentID(id); err == nil {
			t.Errorf("%q: want an error", id)
		}
	}
}
