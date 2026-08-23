package config_test

import (
	"errors"
	"testing"

	"github.com/PedroKlein/duto-ai/internal/config"
)

func TestDecodeConfig_ToolConfigIsStrictAndExpandsTrustedScalars(t *testing.T) {
	t.Setenv("DUTO_TEST_TOKEN", "expanded-token")

	value, err := config.DecodeConfig("duto.yaml", []byte(minimalConfig+`workspaces:
  source: {root: /workspace, access: read}
tool_config:
  files: {workspace: source}
  git: {workspace: source, refs: [HEAD], allow_working_tree: true, max_log_count: 20}
  github:
    base_url: https://api.example.test
    token: ${DUTO_TEST_TOKEN}
    owner: example-owner
    repository: example-repository
    subject: 7
    ref: example-ref
    max_pages: 2
    max_results: 25
  web: {allowed_domains: [example.test], max_redirects: 1}
  shell:
    executable: /bin/echo
    args: [hello]
    workspace: source
    environment: {LANG: C}
    max_stdout_bytes: 128
    max_stderr_bytes: 128
`))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}

	if value.ToolConfig.Files == nil || value.ToolConfig.Files.Workspace != "source" || value.ToolConfig.Git == nil || !value.ToolConfig.Git.AllowWorkingTree {
		t.Fatalf("file/Git tool config = %#v", value.ToolConfig)
	}

	if value.ToolConfig.GitHub == nil || value.ToolConfig.GitHub.Token != "expanded-token" || value.ToolConfig.GitHub.Subject != 7 {
		t.Fatalf("GitHub tool config = %#v", value.ToolConfig.GitHub)
	}

	if value.ToolConfig.Web == nil || value.ToolConfig.Web.AllowedDomains[0] != "example.test" || value.ToolConfig.Shell == nil || value.ToolConfig.Shell.Environment["LANG"] != "C" {
		t.Fatalf("web/shell tool config = %#v", value.ToolConfig)
	}

	_, err = config.DecodeConfig("duto.yaml", []byte(minimalConfig+`tool_config:
  web: {allowed_domains: [example.test], max_redirects: 1, method: POST}
`))

	var diagnostic *config.DiagnosticError
	if !errors.As(err, &diagnostic) || diagnostic.Code != config.CodeUnknownField || diagnostic.Path != "$.tool_config.web.method" {
		t.Fatalf("strict tool config error = %v", err)
	}
}
