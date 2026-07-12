//go:build smoke

package smoketest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/PedroKlein/duto-ai/internal/runtime"
)

func TestSmoke_SimpleNoTools(t *testing.T) {
	envOrSkip(t, "AI_CORE_ENDPOINT")
	envOrSkip(t, "AI_CORE_CLIENT_ID")
	envOrSkip(t, "AI_CORE_CLIENT_SECRET")
	envOrSkip(t, "AI_CORE_AUTH_URL")
	envOrSkip(t, "AI_CORE_RESOURCE_GROUP")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := runtime.Run(ctx,
		filepath.Join("testdata", "config_no_tools.yaml"),
		filepath.Join("testdata", "simple_workflow.yaml"),
	)
	if err != nil {
		t.Fatalf("runtime.Run: %v", err)
	}

	t.Log("Simple no-tools workflow completed successfully")
}
