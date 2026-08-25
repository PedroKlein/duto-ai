package files_test

import (
	"testing"
	"time"

	dtool "github.com/PedroKlein/duto-ai/internal/tool"
	"github.com/PedroKlein/duto-ai/internal/tool/files"
)

func TestM3Authoring_RegistersWriterOnlyWithBoundAuthoring(t *testing.T) {
	root := t.TempDir()

	authoring, err := files.NewAuthoring(root, []string{"report.md"}, 1, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer authoring.Close()

	registry := dtool.NewRegistry()

	err = files.RegisterAll(registry, files.Policy{Root: root, Authoring: authoring, Limits: map[string]dtool.ToolLimit{
		"files.write": {MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 2048, MaxResultBytes: 1024},
	}})
	if err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if _, ok := registry.Get("files.write"); !ok {
		t.Fatal("write policy did not construct files.write")
	}
}

func TestM3Authoring_ReadPolicyDoesNotConstructWriter(t *testing.T) {
	registry := dtool.NewRegistry()

	err := files.RegisterAll(registry, files.Policy{Root: t.TempDir(), Limits: map[string]dtool.ToolLimit{
		"files.read": {MaxCalls: 1, Timeout: time.Second, MaxRequestBytes: 1024, MaxResultBytes: 1024},
	}})
	if err != nil {
		t.Fatalf("RegisterAll() error = %v", err)
	}

	if _, ok := registry.Get("files.write"); ok {
		t.Fatal("read-only file policy constructed files.write")
	}
}
