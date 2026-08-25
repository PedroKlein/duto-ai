package git_test

import (
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	gittool "github.com/PedroKlein/duto-ai/internal/tool/git"
)

func TestM3Authoring_RegistersCommitterOnlyWithBoundAuthoring(t *testing.T) {
	repo, base := newAuthoringRepository(t)

	authoring, writer := newBoundAuthoring(t, repo, base, t.TempDir())
	defer writer.Close()

	registry := dtool.NewRegistry()

	err := gittool.RegisterAll(registry, gittool.Policy{
		Root: repo, Refs: []string{"HEAD"}, AllowWorkingTree: true, MaxLogCount: 10, Authoring: authoring,
		Limits: map[string]dtool.ToolLimit{
			"git.write.commit": {MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1024},
		},
	})
	if err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if _, ok := registry.Get("git.write.commit"); !ok {
		t.Fatal("write policy did not construct git.write.commit")
	}
}

func TestM3Authoring_ReadPolicyDoesNotConstructCommitter(t *testing.T) {
	registry := dtool.NewRegistry()
	if err := gittool.RegisterAll(registry, testPolicy(t.TempDir())); err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if _, ok := registry.Get("git.write.commit"); ok {
		t.Fatal("read-only Git policy constructed git.write.commit")
	}
}
