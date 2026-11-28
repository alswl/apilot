package platform

import (
	"testing"
	"time"
)

func TestTimezoneReturnsExplicitLoadableZone(t *testing.T) {
	name := Timezone()
	if _, err := time.LoadLocation(name); name == "" || name == "Local" || err != nil {
		t.Fatalf("expected an explicit loadable timezone, got %q (%v)", name, err)
	}
}
