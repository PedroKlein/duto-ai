package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

const minimalConfig = `version: 1
providers:
  default:
    type: custom-provider
    config:
      endpoint: ${DUTO_TEST_ENDPOINT}
      credential: ${DUTO_TEST_CREDENTIAL}
models:
  light:
    provider: default
    target: example-small-model
`

func TestDecodeConfig_ExpandsProviderScalarsAfterStructure(t *testing.T) {
	t.Setenv("DUTO_TEST_ENDPOINT", "https://example.invalid")
	t.Setenv("DUTO_TEST_CREDENTIAL", "test-credential")

	configValue, err := config.DecodeConfig("duto.yaml", []byte(minimalConfig))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	provider := configValue.Providers["default"]
	if provider.Type != "custom-provider" || provider.Config["endpoint"] != "https://example.invalid" || provider.Config["credential"] != "test-credential" {
		t.Fatalf("DecodeConfig() provider = %#v", provider)
	}

	if model := configValue.Models["light"]; model.Provider != "default" || model.Target != "example-small-model" {
		t.Fatalf("DecodeConfig() model = %#v", model)
	}
}

func TestDecodeConfig_EvidenceDirectoryIsTrustedAndStrict(t *testing.T) {
	t.Setenv("DUTO_TEST_EVIDENCE", "evidence-output")

	data := minimalConfig + "evidence:\n  directory: ${DUTO_TEST_EVIDENCE}\n"

	configValue, err := config.DecodeConfig("duto.yaml", []byte(data))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	if configValue.Evidence.Directory != "evidence-output" {
		t.Fatalf("evidence directory = %q", configValue.Evidence.Directory)
	}

	_, err = config.DecodeConfig("duto.yaml", []byte(data+"  unexpected: true\n"))

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Code != config.CodeUnknownField {
		t.Fatalf("strict evidence error = %v", err)
	}
}

func TestDecodeConfig_StrictAndSecretSafe(t *testing.T) {
	const secret = "canary-secret-must-not-leak"
	t.Setenv("DUTO_TEST_CREDENTIAL", secret)

	data := strings.Replace(minimalConfig, "type: custom-provider", "type: custom-provider\n    unexpected: true", 1)

	_, err := config.DecodeConfig("duto.yaml", []byte(data))
	if err == nil {
		t.Fatal("DecodeConfig() error = nil")
	}

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("DecodeConfig() error type = %T", err)
	}

	if diagnostic.Code != config.CodeUnknownField || diagnostic.Path != "$.providers.default.unexpected" {
		t.Fatalf("diagnostic = (%q, %q)", diagnostic.Code, diagnostic.Path)
	}

	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "${DUTO_TEST_CREDENTIAL}") {
		t.Fatalf("diagnostic leaked trusted scalar: %q", err)
	}
}

func TestLoadConfig_UsesStrictDecoder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duto.yaml")
	if err := os.WriteFile(path, []byte(minimalConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	configValue, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if configValue.Version != 1 {
		t.Fatalf("LoadConfig().Version = %d", configValue.Version)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig("nonexistent.yaml")
	if err == nil {
		t.Fatal("LoadConfig() error = nil")
	}
}
