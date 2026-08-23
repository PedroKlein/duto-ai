package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ToolConfig struct {
	Files  *FilesToolConfig
	Git    *GitToolConfig
	GitHub *GitHubToolConfig
	Web    *WebToolConfig
	Shell  *ShellToolConfig
}

type FilesToolConfig struct {
	Workspace string
}

type GitToolConfig struct {
	Workspace        string
	Refs             []string
	AllowWorkingTree bool
	MaxLogCount      int
}

type GitHubToolConfig struct {
	BaseURL    string
	Token      string
	Owner      string
	Repository string
	Subject    int
	Ref        string
	MaxPages   int
	MaxResults int
}

type WebToolConfig struct {
	AllowedDomains []string
	MaxRedirects   int
}

type ShellToolConfig struct {
	Executable     string
	Args           []string
	Workspace      string
	Environment    map[string]string
	MaxStdoutBytes int
	MaxStderrBytes int
}

func decodeToolConfig(name string, node *yaml.Node) (ToolConfig, error) {
	if node == nil {
		return ToolConfig{}, nil
	}

	fields, err := mappingFields(name, node, "$.tool_config", "files", "git", "github", "web", "shell")
	if err != nil {
		return ToolConfig{}, err
	}

	var result ToolConfig

	if fields["files"] != nil {
		value, decodeErr := decodeFilesToolConfig(name, fields["files"])
		if decodeErr != nil {
			return ToolConfig{}, decodeErr
		}

		result.Files = &value
	}

	if fields["git"] != nil {
		value, decodeErr := decodeGitToolConfig(name, fields["git"])
		if decodeErr != nil {
			return ToolConfig{}, decodeErr
		}

		result.Git = &value
	}

	if fields["github"] != nil {
		value, decodeErr := decodeGitHubToolConfig(name, fields["github"])
		if decodeErr != nil {
			return ToolConfig{}, decodeErr
		}

		result.GitHub = &value
	}

	if fields["web"] != nil {
		value, decodeErr := decodeWebToolConfig(name, fields["web"])
		if decodeErr != nil {
			return ToolConfig{}, decodeErr
		}

		result.Web = &value
	}

	if fields["shell"] != nil {
		value, decodeErr := decodeShellToolConfig(name, fields["shell"])
		if decodeErr != nil {
			return ToolConfig{}, decodeErr
		}

		result.Shell = &value
	}

	expandToolConfig(&result)

	if err := validateToolConfigValues(name, node, result); err != nil {
		return ToolConfig{}, err
	}

	return result, nil
}

func decodeFilesToolConfig(name string, node *yaml.Node) (FilesToolConfig, error) {
	fields, err := mappingFields(name, node, "$.tool_config.files", "workspace")
	if err != nil {
		return FilesToolConfig{}, err
	}

	workspace, err := requiredString(name, fields, "workspace", "$.tool_config.files.workspace")
	if err != nil {
		return FilesToolConfig{}, err
	}

	if workspace == "" {
		return FilesToolConfig{}, diagnostic(name, "$.tool_config.files.workspace", fields["workspace"], CodeInvalidValue)
	}

	return FilesToolConfig{Workspace: workspace}, nil
}

func decodeGitToolConfig(name string, node *yaml.Node) (GitToolConfig, error) {
	path := "$.tool_config.git"

	fields, err := mappingFields(name, node, path, "workspace", "refs", "allow_working_tree", "max_log_count")
	if err != nil {
		return GitToolConfig{}, err
	}

	workspace, err := requiredString(name, fields, "workspace", path+".workspace")
	if err != nil {
		return GitToolConfig{}, err
	}

	refs, err := decodeStringList(name, fields["refs"], path+".refs")
	if err != nil {
		return GitToolConfig{}, err
	}

	allowWorkingTree, err := requiredBool(name, fields, "allow_working_tree", path+".allow_working_tree")
	if err != nil {
		return GitToolConfig{}, err
	}

	maxLogCount, err := requiredInt(name, fields, "max_log_count", path+".max_log_count")
	if err != nil {
		return GitToolConfig{}, err
	}

	if workspace == "" || maxLogCount <= 0 {
		return GitToolConfig{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return GitToolConfig{Workspace: workspace, Refs: refs, AllowWorkingTree: allowWorkingTree, MaxLogCount: maxLogCount}, nil
}

func decodeGitHubToolConfig(name string, node *yaml.Node) (GitHubToolConfig, error) {
	path := "$.tool_config.github"

	fields, err := mappingFields(name, node, path, "base_url", "token", "owner", "repository", "subject", "ref", "max_pages", "max_results")
	if err != nil {
		return GitHubToolConfig{}, err
	}

	values := make(map[string]string, 5)
	for _, field := range []string{"base_url", "owner", "repository", "ref"} {
		values[field], err = requiredString(name, fields, field, path+"."+field)
		if err != nil {
			return GitHubToolConfig{}, err
		}
	}

	values["token"], err = optionalString(name, fields["token"], path+".token")
	if err != nil {
		return GitHubToolConfig{}, err
	}

	subject, err := requiredInt(name, fields, "subject", path+".subject")
	if err != nil {
		return GitHubToolConfig{}, err
	}

	maxPages, err := requiredInt(name, fields, "max_pages", path+".max_pages")
	if err != nil {
		return GitHubToolConfig{}, err
	}

	maxResults, err := requiredInt(name, fields, "max_results", path+".max_results")
	if err != nil {
		return GitHubToolConfig{}, err
	}

	if subject <= 0 || maxPages <= 0 || maxResults <= 0 {
		return GitHubToolConfig{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return GitHubToolConfig{BaseURL: values["base_url"], Token: values["token"], Owner: values["owner"], Repository: values["repository"], Subject: subject, Ref: values["ref"], MaxPages: maxPages, MaxResults: maxResults}, nil
}

func decodeWebToolConfig(name string, node *yaml.Node) (WebToolConfig, error) {
	path := "$.tool_config.web"

	fields, err := mappingFields(name, node, path, "allowed_domains", "max_redirects")
	if err != nil {
		return WebToolConfig{}, err
	}

	domains, err := decodeStringList(name, fields["allowed_domains"], path+".allowed_domains")
	if err != nil {
		return WebToolConfig{}, err
	}

	redirects, err := requiredInt(name, fields, "max_redirects", path+".max_redirects")
	if err != nil {
		return WebToolConfig{}, err
	}

	if len(domains) == 0 || redirects < 0 {
		return WebToolConfig{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return WebToolConfig{AllowedDomains: domains, MaxRedirects: redirects}, nil
}

func decodeShellToolConfig(name string, node *yaml.Node) (ShellToolConfig, error) {
	path := "$.tool_config.shell"

	fields, err := mappingFields(name, node, path, "executable", "args", "workspace", "environment", "max_stdout_bytes", "max_stderr_bytes")
	if err != nil {
		return ShellToolConfig{}, err
	}

	executable, err := requiredString(name, fields, "executable", path+".executable")
	if err != nil {
		return ShellToolConfig{}, err
	}

	args, err := decodeStringList(name, fields["args"], path+".args")
	if err != nil {
		return ShellToolConfig{}, err
	}

	workspace, err := requiredString(name, fields, "workspace", path+".workspace")
	if err != nil {
		return ShellToolConfig{}, err
	}

	environment, err := decodeStringMap(name, fields["environment"], path+".environment", false)
	if err != nil {
		return ShellToolConfig{}, err
	}

	maxStdoutBytes, err := requiredInt(name, fields, "max_stdout_bytes", path+".max_stdout_bytes")
	if err != nil {
		return ShellToolConfig{}, err
	}

	maxStderrBytes, err := requiredInt(name, fields, "max_stderr_bytes", path+".max_stderr_bytes")
	if err != nil {
		return ShellToolConfig{}, err
	}

	if workspace == "" || maxStdoutBytes <= 0 || maxStderrBytes <= 0 {
		return ShellToolConfig{}, diagnostic(name, path, node, CodeInvalidValue)
	}

	return ShellToolConfig{Executable: executable, Args: args, Workspace: workspace, Environment: environment, MaxStdoutBytes: maxStdoutBytes, MaxStderrBytes: maxStderrBytes}, nil
}

func requiredBool(name string, fields map[string]*yaml.Node, field, path string) (bool, error) {
	node := fields[field]
	if node == nil {
		return false, diagnostic(name, path, nil, CodeMissingField)
	}

	if node.Kind != yaml.ScalarNode || node.Tag != yamlBoolTag {
		return false, diagnostic(name, path, node, CodeInvalidType)
	}

	value, err := strconv.ParseBool(node.Value)
	if err != nil {
		return false, diagnostic(name, path, node, CodeInvalidValue)
	}

	return value, nil
}

func validateToolConfigValues(name string, node *yaml.Node, toolConfig ToolConfig) error {
	checks := []struct {
		path  string
		valid bool
	}{
		{path: "$.tool_config.git.refs", valid: validGitBinding(toolConfig.Git)},
		{path: "$.tool_config.github", valid: validGitHubBinding(toolConfig.GitHub)},
		{path: "$.tool_config.web.allowed_domains", valid: validWebBinding(toolConfig.Web)},
		{path: "$.tool_config.shell", valid: validShellBinding(toolConfig.Shell)},
	}
	for _, check := range checks {
		if !check.valid {
			return diagnostic(name, check.path, node, CodeInvalidValue)
		}
	}

	return nil
}

func validGitBinding(binding *GitToolConfig) bool {
	if binding == nil {
		return true
	}

	for _, ref := range binding.Refs {
		if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n") {
			return false
		}
	}

	return true
}

func validGitHubBinding(binding *GitHubToolConfig) bool {
	if binding == nil {
		return true
	}

	endpoint, err := url.Parse(binding.BaseURL)

	return err == nil && endpoint.Scheme == "https" && endpoint.Host != "" && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == "" && validResourceName(binding.Owner) && validResourceName(binding.Repository) && binding.Ref != "" && !strings.ContainsAny(binding.Ref, "\x00\r\n")
}

func validWebBinding(binding *WebToolConfig) bool {
	if binding == nil {
		return true
	}

	for _, domain := range binding.AllowedDomains {
		if domain == "" || strings.ContainsAny(domain, "/:@\x00\r\n") {
			return false
		}
	}

	return true
}

func validShellBinding(binding *ShellToolConfig) bool {
	if binding == nil {
		return true
	}

	if !filepath.IsAbs(binding.Executable) {
		return false
	}

	for _, arg := range binding.Args {
		if strings.ContainsRune(arg, '\x00') {
			return false
		}
	}

	for key, value := range binding.Environment {
		if !validEnvironmentName(key) || strings.ContainsRune(value, '\x00') {
			return false
		}
	}

	return true
}

func validResourceName(value string) bool {
	if value == "" {
		return false
	}

	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("_.-", character) {
			continue
		}

		return false
	}

	return true
}

func validEnvironmentName(value string) bool {
	if value == "" {
		return false
	}

	for index, character := range value {
		if character == '_' || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || index > 0 && character >= '0' && character <= '9' {
			continue
		}

		return false
	}

	return true
}

func expandToolConfig(toolConfig *ToolConfig) {
	if toolConfig.GitHub != nil {
		toolConfig.GitHub.BaseURL = os.Expand(toolConfig.GitHub.BaseURL, os.Getenv)
		toolConfig.GitHub.Token = os.Expand(toolConfig.GitHub.Token, os.Getenv)
		toolConfig.GitHub.Owner = os.Expand(toolConfig.GitHub.Owner, os.Getenv)
		toolConfig.GitHub.Repository = os.Expand(toolConfig.GitHub.Repository, os.Getenv)
		toolConfig.GitHub.Ref = os.Expand(toolConfig.GitHub.Ref, os.Getenv)
	}

	if toolConfig.Web != nil {
		for i := range toolConfig.Web.AllowedDomains {
			toolConfig.Web.AllowedDomains[i] = os.Expand(toolConfig.Web.AllowedDomains[i], os.Getenv)
		}
	}

	if toolConfig.Shell != nil {
		toolConfig.Shell.Executable = os.Expand(toolConfig.Shell.Executable, os.Getenv)
		for i := range toolConfig.Shell.Args {
			toolConfig.Shell.Args[i] = os.Expand(toolConfig.Shell.Args[i], os.Getenv)
		}

		for key, value := range toolConfig.Shell.Environment {
			toolConfig.Shell.Environment[key] = os.Expand(value, os.Getenv)
		}
	}
}
