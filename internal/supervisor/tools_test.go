package supervisor

import (
	"testing"
)

func TestValidateToolID(t *testing.T) {
	if !ValidateToolID(ToolInfindex) {
		t.Fatal("infindex should be valid")
	}
	if ValidateToolID("no-such-tool") {
		t.Fatal("unknown tool should be invalid")
	}
}
