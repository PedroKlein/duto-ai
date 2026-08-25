package runtime_test

import (
	"testing"

	"github.com/PedroKlein/duto-ai/internal/trust"
)

func TestM3Authoring_RuntimeDeniesUnknownMutationContext(t *testing.T) {
	if got := trust.EligibilityFor(trust.ContextUnknown, trust.CapabilityWorkspaceMutate); got != trust.EligibilityDenied {
		t.Fatalf("unknown workspace mutation eligibility = %q, want denied", got)
	}
}
