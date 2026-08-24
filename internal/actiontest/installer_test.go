package actiontest_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	installerVersionTag = "v1.2.3"
	installerToken      = "TOKEN_CANARY_INSTALLER"
)

type installerPlatformCase struct {
	name         string
	runnerOS     string
	runnerArch   string
	supported    bool
	assetName    string
	assetID      int
	failureMatch []string
}

type installerAsset struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type installerRelease struct {
	TagName string           `json:"tag_name"`
	Assets  []installerAsset `json:"assets"`
}

type installerRunOptions struct {
	platform      installerPlatformCase
	releasePath   string
	curlMode      string
	assetOverride map[int]string
	toolShims     map[string]string
	extraEnv      map[string]string
}

type installerRunResult struct {
	result      commandResult
	curlLog     string
	runnerTemp  string
	githubEnv   string
	releasePath string
}

func TestInstaller_MatrixExactCoverage(t *testing.T) {
	cases := installerPlatformCases()
	got := make([]string, 0, len(cases))

	supported := 0
	unsupported := 0

	for _, tc := range cases {
		got = append(got, tc.name)

		if tc.supported {
			supported++
		} else {
			unsupported++
		}
	}

	want := []string{
		"Linux-X64",
		"Linux-ARM64",
		"macOS-X64",
		"macOS-ARM64",
		"Windows",
		"X86",
		"ARM",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing installer matrix behavior: independent matrix assertion must include exact Linux-X64/Linux-ARM64/macOS-X64/macOS-ARM64 plus Windows/X86/ARM negatives\nwant: %v\ngot:  %v", want, got)
	}

	if supported != 4 || unsupported != 3 {
		t.Fatalf("missing installer matrix behavior: expected exact support split 4 positives + 3 negatives, got %d positives + %d negatives", supported, unsupported)
	}
}

func TestInstaller_AuthenticatedTagAssetMetadataAndUniqueAsset(t *testing.T) {
	linuxX64 := installerPlatformByName(t, "Linux-X64")

	t.Run("exact_tag_and_asset_metadata", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{platform: linuxX64})
		assertInstallerSuccess(t, run, "missing installer behavior: authenticated exact tag/asset metadata must install Linux-X64 through release-asset API")

		if !containsAll(run.curlLog, "/releases/tags/"+installerVersionTag) {
			t.Fatalf("missing installer behavior: metadata request must target exact release tag %s\ncurl log:\n%s", installerVersionTag, run.curlLog)
		}

		if strings.Contains(run.curlLog, "/releases/latest") {
			t.Fatalf("missing installer behavior: latest lookup is forbidden\ncurl log:\n%s", run.curlLog)
		}

		if !containsAll(run.curlLog, fmt.Sprintf("/releases/assets/%d", linuxX64.assetID)) {
			t.Fatalf("missing installer behavior: download must use authenticated release-asset API id %d\ncurl log:\n%s", linuxX64.assetID, run.curlLog)
		}

		if !containsAll(run.curlLog, "auth=Authorization: Bearer "+installerToken) {
			t.Fatalf("missing installer behavior: metadata and asset API requests must be authenticated\ncurl log:\n%s", run.curlLog)
		}
	})

	t.Run("reject_prefix_asset_name", func(t *testing.T) {
		releasePath := mutatedReleaseFixturePath(t, installerFixturePath(t, "release-v1.2.3.json"), func(rel *installerRelease) {
			asset := installerRequireAsset(t, rel, linuxX64.assetName)
			asset.Name = "duto-ai_linux_amd64_partial.tar.gz"
		})

		run := runInstaller(t, installerRunOptions{platform: linuxX64, releasePath: releasePath})
		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: prefix-matching asset names must fail closed")
		}

		if !containsAll(run.result.stderr, "missing", linuxX64.assetName) {
			t.Fatalf("missing installer behavior: exact asset-name mismatch must mention expected archive name\nstderr:\n%s", run.result.stderr)
		}
	})

	t.Run("reject_duplicate_matching_asset", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{
			platform:    linuxX64,
			releasePath: installerFixturePath(t, "release-v1.2.3-duplicate-linux-amd64.json"),
		})

		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: duplicate matching release assets must fail closed")
		}

		if !containsAll(run.result.stderr, "duplicate", "asset") {
			t.Fatalf("missing installer behavior: duplicate asset failure must name duplicate/asset metadata ambiguity\nstderr:\n%s", run.result.stderr)
		}
	})
}

func TestInstaller_DownloadHandles200And302RedirectSafety(t *testing.T) {
	linuxX64 := installerPlatformByName(t, "Linux-X64")

	t.Run("asset_api_stream_200", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{platform: linuxX64, curlMode: "direct200"})
		assertInstallerSuccess(t, run, "missing installer behavior: installer must handle release-asset API 200 stream downloads")

		if !containsAll(run.curlLog, fmt.Sprintf("url=https://api.example.invalid/repos/%s/%s/releases/assets/%d auth=Authorization: Bearer %s", defaultRepositoryOwner, defaultRepositoryName, linuxX64.assetID, installerToken)) {
			t.Fatalf("missing installer behavior: direct 200 download must authenticate release-asset API request\ncurl log:\n%s", run.curlLog)
		}

		if strings.Contains(run.curlLog, "url=https://downloads.example.invalid/") {
			t.Fatalf("missing installer behavior: direct 200 path must not perform redirect-target request\ncurl log:\n%s", run.curlLog)
		}
	})

	t.Run("asset_api_redirect_302", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{platform: linuxX64, curlMode: "redirect302"})
		assertInstallerSuccess(t, run, "missing installer behavior: installer must handle release-asset API 302 redirects")

		assetAPIURL := fmt.Sprintf("url=https://api.example.invalid/repos/%s/%s/releases/assets/%d auth=Authorization: Bearer %s", defaultRepositoryOwner, defaultRepositoryName, linuxX64.assetID, installerToken)
		if !containsAll(run.curlLog, assetAPIURL) {
			t.Fatalf("missing installer behavior: redirect path must authenticate first release-asset API request\ncurl log:\n%s", run.curlLog)
		}

		redirectURL := fmt.Sprintf("url=https://downloads.example.invalid/%s auth=", linuxX64.assetName)
		if !containsAll(run.curlLog, redirectURL) {
			t.Fatalf("missing installer behavior: redirect path must request redirect target without authorization\ncurl log:\n%s", run.curlLog)
		}

		if strings.Contains(run.curlLog, fmt.Sprintf("url=https://downloads.example.invalid/%s auth=Authorization: Bearer %s", linuxX64.assetName, installerToken)) {
			t.Fatalf("missing installer behavior: redirect target request must never include authorization\ncurl log:\n%s", run.curlLog)
		}
	})

	t.Run("reject_302_missing_location", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{platform: linuxX64, curlMode: "redirect302-missing-location"})
		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: redirect without Location must fail closed")
		}

		if !containsAll(run.result.stderr, "redirect", "location") {
			t.Fatalf("missing installer behavior: missing redirect location must mention redirect/location\nstderr:\n%s", run.result.stderr)
		}
	})

	t.Run("reject_302_unsafe_location", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{platform: linuxX64, curlMode: "redirect302-unsafe-location"})
		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: unsafe redirect location must fail closed")
		}

		if !containsAll(run.result.stderr, "redirect", "unsafe") {
			t.Fatalf("missing installer behavior: unsafe redirect failure must mention redirect safety\nstderr:\n%s", run.result.stderr)
		}
	})
}

func TestInstaller_SizeDigestAndPartialDownloadFailures(t *testing.T) {
	linuxX64 := installerPlatformByName(t, "Linux-X64")

	t.Run("reject_wrong_declared_size", func(t *testing.T) {
		releasePath := mutatedReleaseFixturePath(t, installerFixturePath(t, "release-v1.2.3.json"), func(rel *installerRelease) {
			asset := installerRequireAsset(t, rel, linuxX64.assetName)
			asset.Size++
		})

		run := runInstaller(t, installerRunOptions{platform: linuxX64, releasePath: releasePath})
		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: declared size mismatch must fail before extraction")
		}

		if !containsAll(run.result.stderr, "size") {
			t.Fatalf("missing installer behavior: declared size mismatch failure must name size verification\nstderr:\n%s", run.result.stderr)
		}
	})

	t.Run("reject_wrong_sha256_digest", func(t *testing.T) {
		releasePath := mutatedReleaseFixturePath(t, installerFixturePath(t, "release-v1.2.3.json"), func(rel *installerRelease) {
			asset := installerRequireAsset(t, rel, linuxX64.assetName)
			asset.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
		})

		run := runInstaller(t, installerRunOptions{platform: linuxX64, releasePath: releasePath})
		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: sha256 digest mismatch must fail before extraction")
		}

		if !containsAll(run.result.stderr, "sha256") {
			t.Fatalf("missing installer behavior: digest mismatch failure must name sha256 verification\nstderr:\n%s", run.result.stderr)
		}
	})

	t.Run("reject_partial_download", func(t *testing.T) {
		run := runInstaller(t, installerRunOptions{
			platform: linuxX64,
			assetOverride: map[int]string{
				linuxX64.assetID: installerAssetFixturePath(t, "duto-ai_linux_amd64_partial.tar.gz"),
			},
		})

		if run.result.exitCode == 0 {
			t.Fatalf("missing installer behavior: partial download must fail size/digest verification")
		}

		if !containsAll(run.result.stderr, "size") && !containsAll(run.result.stderr, "sha256") {
			t.Fatalf("missing installer behavior: partial download failure must name size or digest verification\nstderr:\n%s", run.result.stderr)
		}
	})
}

func TestInstaller_ArchiveConfinementFailures(t *testing.T) {
	linuxX64 := installerPlatformByName(t, "Linux-X64")

	tests := []struct {
		name          string
		assetFixture  string
		errorContains []string
	}{
		{
			name:          "reject_symlink_member",
			assetFixture:  "duto-ai_linux_amd64_symlink.tar.gz",
			errorContains: []string{"regular", "symlink"},
		},
		{
			name:          "reject_traversal_member",
			assetFixture:  "duto-ai_linux_amd64_traversal.tar.gz",
			errorContains: []string{"traversal", ".."},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			releasePath := mutatedReleaseFixturePath(t, installerFixturePath(t, "release-v1.2.3.json"), func(rel *installerRelease) {
				asset := installerRequireAsset(t, rel, linuxX64.assetName)
				size, digest := installerFileSizeAndDigest(t, installerAssetFixturePath(t, tc.assetFixture))
				asset.Size = size
				asset.Digest = digest
			})

			run := runInstaller(t, installerRunOptions{
				platform:    linuxX64,
				releasePath: releasePath,
				assetOverride: map[int]string{
					linuxX64.assetID: installerAssetFixturePath(t, tc.assetFixture),
				},
			})
			if run.result.exitCode == 0 {
				t.Fatalf("missing installer behavior: archive confinement must reject %s", tc.name)
			}

			if !containsAny(run.result.stderr, tc.errorContains...) {
				t.Fatalf("missing installer behavior: archive confinement failure must mention one of %v\nstderr:\n%s", tc.errorContains, run.result.stderr)
			}
		})
	}
}

func TestInstaller_ToolAndPlatformFailures(t *testing.T) {
	t.Run("unsupported_platform_negatives", func(t *testing.T) {
		for _, tc := range installerPlatformCases() {
			if tc.supported {
				continue
			}

			t.Run(tc.name, func(t *testing.T) {
				run := runInstaller(t, installerRunOptions{platform: tc})
				if run.result.exitCode == 0 {
					t.Fatalf("missing installer behavior: unsupported platform case %s must fail closed", tc.name)
				}

				if len(tc.failureMatch) > 0 && !containsAny(run.result.stderr, tc.failureMatch...) {
					t.Fatalf("missing installer behavior: unsupported platform failure must mention %v\nstderr:\n%s", tc.failureMatch, run.result.stderr)
				}
			})
		}
	})

	t.Run("tool_failures", func(t *testing.T) {
		linuxX64 := installerPlatformByName(t, "Linux-X64")

		tests := []struct {
			name          string
			toolShims     map[string]string
			errorContains []string
		}{
			{
				name: "missing_jq",
				toolShims: map[string]string{
					"jq": installerBrokenToolShim("jq"),
				},
				errorContains: []string{"jq"},
			},
			{
				name: "missing_tar",
				toolShims: map[string]string{
					"tar": installerBrokenToolShim("tar"),
				},
				errorContains: []string{"tar"},
			},
			{
				name: "missing_checksum_tool",
				toolShims: map[string]string{
					"sha256sum": installerBrokenToolShim("sha256sum"),
					"shasum":    installerBrokenToolShim("shasum"),
					"openssl":   installerBrokenToolShim("openssl"),
				},
				errorContains: []string{"sha", "checksum"},
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				run := runInstaller(t, installerRunOptions{platform: linuxX64, toolShims: tc.toolShims})
				if run.result.exitCode == 0 {
					t.Fatalf("missing installer behavior: %s must fail closed", tc.name)
				}

				if !containsAny(run.result.stderr, tc.errorContains...) {
					t.Fatalf("missing installer behavior: tool failure must mention one of %v\nstderr:\n%s", tc.errorContains, run.result.stderr)
				}
			})
		}
	})
}

func installerPlatformCases() []installerPlatformCase {
	return []installerPlatformCase{
		{
			name:       "Linux-X64",
			runnerOS:   "Linux",
			runnerArch: "X64",
			supported:  true,
			assetName:  "duto-ai_linux_amd64.tar.gz",
			assetID:    101,
		},
		{
			name:       "Linux-ARM64",
			runnerOS:   "Linux",
			runnerArch: "ARM64",
			supported:  true,
			assetName:  "duto-ai_linux_arm64.tar.gz",
			assetID:    102,
		},
		{
			name:       "macOS-X64",
			runnerOS:   "macOS",
			runnerArch: "X64",
			supported:  true,
			assetName:  "duto-ai_darwin_amd64.tar.gz",
			assetID:    103,
		},
		{
			name:       "macOS-ARM64",
			runnerOS:   "macOS",
			runnerArch: "ARM64",
			supported:  true,
			assetName:  "duto-ai_darwin_arm64.tar.gz",
			assetID:    104,
		},
		{
			name:         "Windows",
			runnerOS:     "Windows",
			runnerArch:   "X64",
			supported:    false,
			failureMatch: []string{"windows", "unsupported"},
		},
		{
			name:         "X86",
			runnerOS:     "Linux",
			runnerArch:   "X86",
			supported:    false,
			failureMatch: []string{"x86", "unsupported"},
		},
		{
			name:         "ARM",
			runnerOS:     "Linux",
			runnerArch:   "ARM",
			supported:    false,
			failureMatch: []string{"arm", "unsupported"},
		},
	}
}

func installerPlatformByName(t *testing.T, name string) installerPlatformCase {
	t.Helper()

	for _, tc := range installerPlatformCases() {
		if tc.name == name {
			return tc
		}
	}

	t.Fatalf("installer test setup failure: unknown platform case %q", name)

	return installerPlatformCase{}
}

func runInstaller(t *testing.T, opts installerRunOptions) installerRunResult {
	t.Helper()

	if opts.releasePath == "" {
		opts.releasePath = installerFixturePath(t, "release-v1.2.3.json")
	}

	if opts.curlMode == "" {
		opts.curlMode = "direct200"
	}

	if opts.platform.name == "" {
		opts.platform = installerPlatformByName(t, "Linux-X64")
	}

	release := readInstallerReleaseFixture(t, opts.releasePath)

	root := repoRoot(t)
	installScript := filepath.Join(root, "action", "install.sh")

	shimDir := t.TempDir()
	writeInstallerShim(t, shimDir, "curl", installerCurlShimScript())

	for name, content := range opts.toolShims {
		writeInstallerShim(t, shimDir, name, content)
	}

	runnerTemp := filepath.Join(t.TempDir(), "runner-temp")
	if err := os.MkdirAll(runnerTemp, 0o755); err != nil {
		t.Fatalf("installer setup failure: create runner temp: %v", err)
	}

	githubEnv := filepath.Join(t.TempDir(), "github-env.txt")
	if err := os.WriteFile(githubEnv, nil, 0o600); err != nil {
		t.Fatalf("installer setup failure: create GITHUB_ENV file: %v", err)
	}

	curlLog := filepath.Join(t.TempDir(), "curl-log.txt")
	curlCount := filepath.Join(t.TempDir(), "curl-count.txt")

	envMap := map[string]string{
		"PATH":                      shimDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME":                      os.Getenv("HOME"),
		"RUNNER_TEMP":               runnerTemp,
		"RUNNER_OS":                 opts.platform.runnerOS,
		"RUNNER_ARCH":               opts.platform.runnerArch,
		"GITHUB_REPOSITORY":         defaultRepositoryOwner + "/" + defaultRepositoryName,
		"GITHUB_REPOSITORY_OWNER":   defaultRepositoryOwner,
		"GITHUB_API_URL":            "https://api.example.invalid",
		"GITHUB_SERVER_URL":         "https://github.example.invalid",
		"GITHUB_TOKEN":              installerToken,
		"INPUT_VERSION":             installerVersionTag,
		"GITHUB_ENV":                githubEnv,
		"DUTO_FAKE_CURL_LOG_FILE":   curlLog,
		"DUTO_FAKE_CURL_COUNT_FILE": curlCount,
		"DUTO_FAKE_CURL_MODE":       opts.curlMode,
		"DUTO_FAKE_RELEASE_JSON":    opts.releasePath,
		"DUTO_FAKE_ASSET_DIR":       filepath.Join(filepath.Dir(opts.releasePath), "assets"),
	}

	if tmp := os.Getenv("TMPDIR"); strings.TrimSpace(tmp) != "" {
		envMap["TMPDIR"] = tmp
	}

	for _, asset := range release.Assets {
		assetPath := installerAssetFixturePath(t, asset.Name)
		if override, ok := opts.assetOverride[asset.ID]; ok {
			assetPath = override
		}

		envMap[fmt.Sprintf("DUTO_FAKE_ASSET_%d", asset.ID)] = assetPath
	}

	maps.Copy(envMap, opts.extraEnv)

	cmd := exec.Command("bash", installScript)
	cmd.Dir = root
	cmd.Env = installerEnvironment(envMap)

	result := runCommand(t, cmd, nil)

	curlLogBytes, err := os.ReadFile(curlLog)

	logText := ""
	if err == nil {
		logText = string(curlLogBytes)
	}

	return installerRunResult{
		result:      result,
		curlLog:     logText,
		runnerTemp:  runnerTemp,
		githubEnv:   githubEnv,
		releasePath: opts.releasePath,
	}
}

func installerEnvironment(extra map[string]string) []string {
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, fmt.Sprintf("%s=%s", key, extra[key]))
	}

	return env
}

func writeInstallerShim(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("installer setup failure: write %s shim: %v", name, err)
	}
}

func installerCurlShimScript() string {
	return `#!/usr/bin/env bash
set -euo pipefail

log="${DUTO_FAKE_CURL_LOG_FILE:?}"
count_file="${DUTO_FAKE_CURL_COUNT_FILE:?}"
mode="${DUTO_FAKE_CURL_MODE:-direct200}"

count=0
if [[ -f "$count_file" ]]; then
  count="$(cat "$count_file")"
fi
count=$((count + 1))
printf '%s' "$count" > "$count_file"

url=""
output=""
auth=""
accept=""
dump_header=""
write_out=""
args="$*"

while (($#)); do
  case "$1" in
    -o|--output)
      shift
      output="${1:-}"
      ;;
    -H|--header)
      shift
      header="${1:-}"
      case "$header" in
        Authorization:*) auth="$header" ;;
        Accept:*) accept="$header" ;;
      esac
      ;;
    -D|--dump-header)
      shift
      dump_header="${1:-}"
      ;;
    -w|--write-out)
      shift
      write_out="${1:-}"
      ;;
    http://*|https://*)
      url="$1"
      ;;
  esac
  shift || true
done

printf 'call=%s mode=%s url=%s auth=%s accept=%s output=%s dump_header=%s write_out=%s args=%s\n' "$count" "$mode" "$url" "$auth" "$accept" "$output" "$dump_header" "$write_out" "$args" >> "$log"

emit_file() {
  local src="$1"
  if [[ -n "$output" ]]; then
    cat "$src" > "$output"
  else
    cat "$src"
  fi
}

emit_headers() {
  local status="$1"
  local location="${2:-}"
  if [[ -z "$dump_header" ]]; then
    return
  fi

  {
    printf 'HTTP/1.1 %s\r\n' "$status"
    if [[ -n "$location" ]]; then
      printf 'Location: %s\r\n' "$location"
    fi
    printf '\r\n'
  } > "$dump_header"
}

emit_status() {
  local status="$1"
  if [[ "$write_out" == "%{http_code}" ]]; then
    printf '%s' "$status"
  fi
}

if [[ "$url" == *"/releases/tags/"* ]]; then
  emit_headers "200"
  emit_file "${DUTO_FAKE_RELEASE_JSON:?}"
  emit_status "200"
  exit 0
fi

if [[ "$url" == *"/releases/assets/"* ]]; then
  if [[ "$mode" == redirect302* && ( "$args" == *" -L "* || "$args" == *" --location "* ) && -n "$auth" ]]; then
    echo "redirect auth leak: location-follow with authorization header is forbidden" >&2
    exit 9
  fi

  if [[ -z "$auth" ]]; then
    echo "release asset API request missing authorization" >&2
    exit 11
  fi

  asset_id="${url##*/}"
  asset_var="DUTO_FAKE_ASSET_${asset_id}"
  asset_path="${!asset_var:-}"
  if [[ -z "$asset_path" ]]; then
    echo "unknown asset id: ${asset_id}" >&2
    exit 8
  fi

  case "$mode" in
    direct200)
      emit_headers "200"
      emit_file "$asset_path"
      emit_status "200"
      exit 0
      ;;
    redirect302)
      redirect_url="https://downloads.example.invalid/$(basename "$asset_path")"
      emit_headers "302" "$redirect_url"
      : > "${output:-/dev/null}"
      emit_status "302"
      exit 0
      ;;
    redirect302-missing-location)
      emit_headers "302"
      : > "${output:-/dev/null}"
      emit_status "302"
      exit 0
      ;;
    redirect302-unsafe-location)
      redirect_url="http://downloads.example.invalid/$(basename "$asset_path")"
      emit_headers "302" "$redirect_url"
      : > "${output:-/dev/null}"
      emit_status "302"
      exit 0
      ;;
    *)
      echo "unknown fake curl mode: ${mode}" >&2
      exit 12
      ;;
  esac
fi

if [[ "$url" == https://downloads.example.invalid/* || "$url" == http://downloads.example.invalid/* ]]; then
  if [[ -n "$auth" ]]; then
    echo "redirected host received auth header" >&2
    exit 10
  fi

  name="${url##*/}"
  emit_headers "200"
  emit_file "${DUTO_FAKE_ASSET_DIR:?}/${name}"
  emit_status "200"
  exit 0
fi

echo "unhandled url: ${url}" >&2
exit 7
`
}

func installerBrokenToolShim(tool string) string {
	return fmt.Sprintf("#!/usr/bin/env bash\nset -euo pipefail\necho '%s unavailable from test shim' >&2\nexit 127\n", tool)
}

func assertInstallerSuccess(t *testing.T, run installerRunResult, context string) {
	t.Helper()

	if run.result.exitCode != 0 {
		t.Fatalf("%s\nexit=%d\nstdout:\n%s\nstderr:\n%s", context, run.result.exitCode, run.result.stdout, run.result.stderr)
	}

	installed := findInstalledBinaries(t, run.runnerTemp)
	if len(installed) == 0 {
		t.Fatalf("%s\nexpected one installed duto-ai binary under RUNNER_TEMP=%s", context, run.runnerTemp)
	}
}

func findInstalledBinaries(t *testing.T, root string) []string {
	t.Helper()

	out := make([]string, 0)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		if filepath.Base(path) != "duto-ai" {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if !info.Mode().IsRegular() {
			return nil
		}

		out = append(out, path)

		return nil
	})
	if err != nil {
		t.Fatalf("installer setup failure: walk RUNNER_TEMP %s: %v", root, err)
	}

	return out
}

func installerFixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(repoRoot(t), "internal", "actiontest", "testdata", "installer", name)
}

func installerAssetFixturePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(installerFixturePath(t, "assets"), name)
}

func readInstallerReleaseFixture(t *testing.T, path string) installerRelease {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("installer setup failure: read release fixture %s: %v", path, err)
	}

	var rel installerRelease
	if err := json.Unmarshal(content, &rel); err != nil {
		t.Fatalf("installer setup failure: parse release fixture %s: %v", path, err)
	}

	return rel
}

func mutatedReleaseFixturePath(t *testing.T, basePath string, mutate func(*installerRelease)) string {
	t.Helper()

	rel := readInstallerReleaseFixture(t, basePath)
	mutate(&rel)

	outPath := filepath.Join(t.TempDir(), filepath.Base(basePath))

	content, err := json.MarshalIndent(rel, "", "  ")
	if err != nil {
		t.Fatalf("installer setup failure: marshal mutated release fixture: %v", err)
	}

	if err := os.WriteFile(outPath, content, 0o644); err != nil {
		t.Fatalf("installer setup failure: write mutated release fixture: %v", err)
	}

	return outPath
}

func installerRequireAsset(t *testing.T, rel *installerRelease, name string) *installerAsset {
	t.Helper()

	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}

	t.Fatalf("installer setup failure: release fixture missing asset %q", name)

	return nil
}

func installerFileSizeAndDigest(t *testing.T, path string) (int64, string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("installer setup failure: read asset fixture %s: %v", path, err)
	}

	sum := sha256.Sum256(content)

	return int64(len(content)), fmt.Sprintf("sha256:%x", sum)
}

func containsAny(haystack string, parts ...string) bool {
	lower := strings.ToLower(haystack)
	for _, part := range parts {
		if strings.Contains(lower, strings.ToLower(part)) {
			return true
		}
	}

	return false
}
