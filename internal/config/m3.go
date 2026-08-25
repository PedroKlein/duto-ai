package config

import (
	"net/mail"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	m3BranchPrefix       = "duto/m3/"
	maxM3ChangedFiles    = 64
	maxM3FileBytes       = 1_048_576
	maxM3TotalWriteBytes = 8_388_608
	maxM3CommitMessage   = 4_096
	maxM3ReplyBytes      = 32_768
	maxM3PRTitleBytes    = 256
	maxM3PRBodyBytes     = 65_536
	maxM3BundleBytes     = 16_777_216
)

func decodeM3Config(name string, fields map[string]*yaml.Node, workspaces map[string]Workspace, toolLimits map[string]ToolLimit) (*M3, Evidence, error) {
	m3, err := decodeM3(name, fields["m3"], workspaces)
	if err != nil {
		return nil, Evidence{}, err
	}

	evidence, err := decodeEvidence(name, fields["evidence"])
	if err != nil {
		return nil, Evidence{}, err
	}

	if m3 != nil && evidence.Directory == "" {
		return nil, Evidence{}, diagnostic(name, "$.evidence.directory", fields["evidence"], CodeMissingField)
	}

	if m3 != nil && !validM3ToolLimits(toolLimits) {
		return nil, Evidence{}, diagnostic(name, "$.tool_limits", fields["tool_limits"], CodeInvalidValue)
	}

	return m3, evidence, nil
}

func validM3ToolLimits(limits map[string]ToolLimit) bool {
	maximums := map[string]ToolLimit{
		"files.write":                    {MaxCalls: 64, Timeout: "15s", MaxRequestBytes: 1_049_600, MaxResultBytes: 4_096},
		"git.write.commit":               {MaxCalls: 1, Timeout: "30s", MaxRequestBytes: 65_536, MaxResultBytes: 8_192},
		"safe-output.conversation-reply": {MaxCalls: 1, Timeout: "5s", MaxRequestBytes: 33_792, MaxResultBytes: 4_096},
		"safe-output.branch":             {MaxCalls: 1, Timeout: "5s", MaxRequestBytes: 1_024, MaxResultBytes: 4_096},
		"safe-output.draft-pr":           {MaxCalls: 1, Timeout: "5s", MaxRequestBytes: 67_584, MaxResultBytes: 4_096},
	}
	for name, maximum := range maximums {
		value, exists := limits[name]
		if !exists {
			continue
		}

		valueTimeout, valueErr := time.ParseDuration(value.Timeout)
		maximumTimeout, _ := time.ParseDuration(maximum.Timeout)

		if valueErr != nil || value.MaxCalls <= 0 || value.MaxCalls > maximum.MaxCalls || valueTimeout <= 0 || valueTimeout > maximumTimeout ||
			value.MaxRequestBytes <= 0 || value.MaxRequestBytes > maximum.MaxRequestBytes || value.MaxResultBytes <= 0 || value.MaxResultBytes > maximum.MaxResultBytes {
			return false
		}
	}

	return true
}

func decodeM3(name string, node *yaml.Node, workspaces map[string]Workspace) (*M3, error) {
	if node == nil {
		for workspaceName, workspace := range workspaces {
			if workspace.Access == WorkspaceAccessWrite {
				return nil, diagnostic(name, "$.workspaces."+workspaceName+".access", nil, CodeInvalidValue)
			}
		}

		return nil, nil
	}

	fields, err := mappingFields(name, node, "$.m3", "admission", "authoring", "publication")
	if err != nil {
		return nil, err
	}

	admission, err := decodeM3Admission(name, fields["admission"])
	if err != nil {
		return nil, err
	}

	authoring, err := decodeM3Authoring(name, fields["authoring"])
	if err != nil {
		return nil, err
	}

	publication, err := decodeM3Publication(name, fields["publication"])
	if err != nil {
		return nil, err
	}

	writeCount := 0

	for workspaceName, workspace := range workspaces {
		if workspace.Access != WorkspaceAccessWrite {
			continue
		}

		writeCount++

		if workspaceName != authoring.Workspace {
			return nil, diagnostic(name, "$.m3.authoring.workspace", fields["authoring"], CodeInvalidValue)
		}
	}

	workspace, exists := workspaces[authoring.Workspace]
	if !exists || workspace.Access != WorkspaceAccessWrite || writeCount != 1 {
		return nil, diagnostic(name, "$.m3.authoring.workspace", fields["authoring"], CodeInvalidValue)
	}

	return &M3{Admission: admission, Authoring: authoring, Publication: publication}, nil
}

func decodeM3Admission(name string, node *yaml.Node) (M3Admission, error) {
	const base = "$.m3.admission"

	fields, err := mappingFields(name, node, base, "id", "contexts", "capabilities")
	if err != nil {
		return M3Admission{}, err
	}

	id, err := requiredString(name, fields, "id", base+".id")
	if err != nil {
		return M3Admission{}, err
	}

	contexts, err := decodeStringList(name, fields["contexts"], base+".contexts")
	if err != nil {
		return M3Admission{}, err
	}

	capabilities, err := decodeStringList(name, fields["capabilities"], base+".capabilities")
	if err != nil {
		return M3Admission{}, err
	}

	if !namePattern.MatchString(id) || !validUniqueSubset(contexts, []string{"same_repository", "scheduled", "local"}) ||
		!validUniqueSubset(capabilities, []string{"workspace.mutate", "git.mutate", "git.publish", "process.exec", "github.mutate"}) {
		return M3Admission{}, diagnostic(name, base, node, CodeInvalidValue)
	}

	return M3Admission{ID: id, Contexts: contexts, Capabilities: capabilities}, nil
}

func decodeM3Authoring(name string, node *yaml.Node) (M3Authoring, error) {
	const base = "$.m3.authoring"

	fields, err := mappingFields(name, node, base, "workspace", "allowed_paths", "max_changed_files", "max_file_bytes", "max_total_write_bytes", "max_commit_message_bytes", "commit_author_name", "commit_author_email")
	if err != nil {
		return M3Authoring{}, err
	}

	workspace, err := requiredString(name, fields, "workspace", base+".workspace")
	if err != nil {
		return M3Authoring{}, err
	}

	allowedPaths, err := decodeStringList(name, fields["allowed_paths"], base+".allowed_paths")
	if err != nil {
		return M3Authoring{}, err
	}

	values, err := decodeM3Bounds(name, fields, base, []string{"max_changed_files", "max_file_bytes", "max_total_write_bytes", "max_commit_message_bytes"})
	if err != nil {
		return M3Authoring{}, err
	}

	authorName, err := requiredString(name, fields, "commit_author_name", base+".commit_author_name")
	if err != nil {
		return M3Authoring{}, err
	}

	authorEmail, err := requiredString(name, fields, "commit_author_email", base+".commit_author_email")
	if err != nil {
		return M3Authoring{}, err
	}

	if workspace == "" || !validAllowedPaths(allowedPaths) || !within(values[0], maxM3ChangedFiles) ||
		!within(values[1], maxM3FileBytes) || !within(values[2], maxM3TotalWriteBytes) ||
		!within(values[3], maxM3CommitMessage) || invalidControlString(authorName) || !validEmail(authorEmail) {
		return M3Authoring{}, diagnostic(name, base, node, CodeInvalidValue)
	}

	return M3Authoring{
		Workspace: workspace, AllowedPaths: allowedPaths, MaxChangedFiles: values[0], MaxFileBytes: values[1],
		MaxTotalWriteBytes: values[2], MaxCommitMessageBytes: values[3], CommitAuthorName: authorName, CommitAuthorEmail: authorEmail,
	}, nil
}

func decodeM3Publication(name string, node *yaml.Node) (M3Publication, error) {
	const base = "$.m3.publication"

	fields, err := mappingFields(name, node, base, "mode", "operation_sets", "branch_prefix", "max_reply_bytes", "max_pr_title_bytes", "max_pr_body_bytes", "max_bundle_bytes")
	if err != nil {
		return M3Publication{}, err
	}

	mode, err := requiredString(name, fields, "mode", base+".mode")
	if err != nil {
		return M3Publication{}, err
	}

	operationSets, err := decodeStringList(name, fields["operation_sets"], base+".operation_sets")
	if err != nil {
		return M3Publication{}, err
	}

	branchPrefix, err := requiredString(name, fields, "branch_prefix", base+".branch_prefix")
	if err != nil {
		return M3Publication{}, err
	}

	values, err := decodeM3Bounds(name, fields, base, []string{"max_reply_bytes", "max_pr_title_bytes", "max_pr_body_bytes", "max_bundle_bytes"})
	if err != nil {
		return M3Publication{}, err
	}

	if mode != "staged" || branchPrefix != m3BranchPrefix || !validUniqueSubset(operationSets, []string{"conversation-reply", "branch-pr"}) ||
		!within(values[0], maxM3ReplyBytes) || !within(values[1], maxM3PRTitleBytes) ||
		!within(values[2], maxM3PRBodyBytes) || !within(values[3], maxM3BundleBytes) {
		return M3Publication{}, diagnostic(name, base, node, CodeInvalidValue)
	}

	return M3Publication{Mode: mode, OperationSets: operationSets, BranchPrefix: branchPrefix, MaxReplyBytes: values[0], MaxPRTitleBytes: values[1], MaxPRBodyBytes: values[2], MaxBundleBytes: values[3]}, nil
}

func decodeM3Bounds(name string, fields map[string]*yaml.Node, base string, names []string) ([]int, error) {
	values := make([]int, len(names))
	for i, field := range names {
		value, err := requiredInt(name, fields, field, base+"."+field)
		if err != nil {
			return nil, err
		}

		values[i] = value
	}

	return values, nil
}

func validUniqueSubset(values, allowed []string) bool {
	if len(values) == 0 {
		return false
	}

	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !slices.Contains(allowed, value) {
			return false
		}

		if _, exists := seen[value]; exists {
			return false
		}

		seen[value] = struct{}{}
	}

	return true
}

func validAllowedPaths(values []string) bool {
	if len(values) == 0 || len(values) > maxM3ChangedFiles {
		return false
	}

	for _, value := range values {
		if !validAllowedPath(value) {
			return false
		}
	}

	return true
}

func validAllowedPath(value string) bool {
	if value == "" || len(value) > 1024 || !utf8.ValidString(value) || path.IsAbs(value) || strings.ContainsAny(value, "\\\x00") {
		return false
	}

	trimmed := strings.TrimSuffix(value, "/")
	if trimmed == "" || trimmed == ".git" || strings.HasPrefix(trimmed, ".git/") {
		return false
	}

	for part := range strings.SplitSeq(trimmed, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}

	return true
}

func within(value, maximum int) bool {
	return value > 0 && value <= maximum
}

func invalidControlString(value string) bool {
	if value == "" || !utf8.ValidString(value) {
		return true
	}

	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func validEmail(value string) bool {
	if invalidControlString(value) {
		return false
	}

	address, err := mail.ParseAddress(value)

	return err == nil && address.Address == value
}
