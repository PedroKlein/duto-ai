package tool_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
)

func TestResolveScope_ExpandsProfilesAndSelectorsDeterministically(t *testing.T) {
	catalog := dtool.BuiltinCatalog()

	profiles, err := dtool.MergeProfiles(
		map[string][]string{"source-review": {"files.grep", "git.read.*"}},
		map[string][]string{"focused": {"files.read", "github.read.pr"}},
	)
	if err != nil {
		t.Fatalf("MergeProfiles() error = %v", err)
	}

	ceiling, err := catalog.ResolveCeiling([]string{"*"})
	if err != nil {
		t.Fatalf("ResolveCeiling() error = %v", err)
	}

	scope, err := catalog.ResolveScope(dtool.ScopeRequest{
		Expression: dtool.Expression{
			AddProfiles:    []string{"source-review", "focused"},
			Add:            []string{"files.*"},
			RemoveProfiles: []string{"focused"},
			Remove:         []string{"git.read.blame"},
		},
		Profiles: profiles,
		Ceiling:  ceiling,
		Root:     true,
	})
	if err != nil {
		t.Fatalf("ResolveScope() error = %v", err)
	}

	want := []string{"files.find", "files.grep", "git.read.diff", "git.read.log", "git.read.show"}
	if diff := cmp.Diff(want, scope.Names); diff != "" {
		t.Fatalf("resolved names mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveScope_FailsClosedWithStableCodes(t *testing.T) {
	catalog := dtool.BuiltinCatalog()

	ceiling, err := catalog.ResolveCeiling([]string{"files.*", "git.read.*", "github.read.*", "web.fetch", "shell.run"})
	if err != nil {
		t.Fatalf("ResolveCeiling() error = %v", err)
	}

	tests := []struct {
		name    string
		request dtool.ScopeRequest
		code    string
	}{
		{name: "unknown profile", request: dtool.ScopeRequest{Expression: dtool.Expression{AddProfiles: []string{"missing"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeUnknownToolProfile},
		{name: "unknown exact", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"files.missing"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeUnknownTool},
		{name: "broad github wildcard", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"github.*"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeInvalidToolSelector},
		{name: "broad git wildcard", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"git.*"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeInvalidToolSelector},
		{name: "portable global wildcard", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"*"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeInvalidToolSelector},
		{name: "invalid removal", request: dtool.ScopeRequest{Expression: dtool.Expression{Remove: []string{"files.read"}}, Ceiling: ceiling, Root: true}, code: dtool.CodeInvalidToolRemoval},
		{name: "parent at root", request: dtool.ScopeRequest{Expression: dtool.Expression{From: dtool.FromParent}, Ceiling: ceiling, Root: true}, code: dtool.CodeInvalidToolParent},
		{name: "ceiling exceeded", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"shell.run"}}, Ceiling: []string{"files.read"}, Root: true}, code: dtool.CodeToolCeilingExceeded},
		{name: "parent widened", request: dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"files.grep"}}, Ceiling: ceiling, Parent: []string{"files.read"}}, code: dtool.CodeToolAuthorityWidening},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, resolveErr := catalog.ResolveScope(test.request)

			var policyErr *dtool.PolicyError
			if !errors.As(resolveErr, &policyErr) || policyErr.Code != test.code {
				t.Fatalf("ResolveScope() error = %v, want code %q", resolveErr, test.code)
			}
		})
	}
}

func TestResolveScope_ReportsUnmatchedAllowedWildcard(t *testing.T) {
	catalog, err := dtool.NewCatalog([]dtool.Definition{{Name: "web.fetch", Capabilities: []dtool.Capability{dtool.CapabilityNetworkRead}, SideEffect: dtool.SideEffectRead}})
	if err != nil {
		t.Fatalf("NewCatalog() error = %v", err)
	}

	_, err = catalog.ResolveScope(dtool.ScopeRequest{Expression: dtool.Expression{Add: []string{"files.*"}}, Ceiling: []string{"web.fetch"}, Root: true})

	var policyErr *dtool.PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != dtool.CodeUnmatchedToolSelector {
		t.Fatalf("ResolveScope() error = %v", err)
	}
}

func TestMergeProfiles_RejectsCollisions(t *testing.T) {
	_, err := dtool.MergeProfiles(map[string][]string{"review": {"files.read"}}, map[string][]string{"review": {"files.grep"}})

	var policyErr *dtool.PolicyError
	if !errors.As(err, &policyErr) || policyErr.Code != dtool.CodeDuplicateToolProfile {
		t.Fatalf("MergeProfiles() error = %v", err)
	}
}
