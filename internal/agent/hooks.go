package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/randalmurphal/llmkit/claudeconfig"
	"github.com/randalmurphal/llmkit/codexcontract"
)

// hookMatcher is used to identify hooks added by HerdingLlamas so they can
// be removed during cleanup.
const hookMatcher = "herdingllamas-stop-hook"

// ConfigureStopHook writes stop hook configuration for the given provider
// and debate. It writes the hook script to a temporary executable file and
// configures the provider's hook system to invoke it on Stop events.
//
// Returns a cleanup function that removes both the script file and the
// hook configuration entry. The cleanup function is safe to call multiple
// times and ignores removal errors (best-effort cleanup).
//
// For Claude: writes to {workDir}/.claude/settings.json (project-level).
// For Codex: writes to {workDir}/.codex/hooks.json.
func ConfigureStopHook(provider Provider, workDir string, hookScript string) (cleanup func(), err error) {
	// Write the hook script to a temp file inside the debate state dir so
	// it lives alongside other debate artifacts.
	scriptPath, err := writeHookScript(hookScript)
	if err != nil {
		return nil, fmt.Errorf("writing hook script: %w", err)
	}

	var configCleanup func()

	switch provider {
	case ProviderClaude:
		configCleanup, err = configureClaudeStopHook(workDir, scriptPath)
	case ProviderCodex:
		configCleanup, err = configureCodexStopHook(workDir, scriptPath)
	default:
		os.Remove(scriptPath)
		return nil, fmt.Errorf("unsupported provider for stop hooks: %q", provider)
	}

	if err != nil {
		os.Remove(scriptPath)
		return nil, err
	}

	cleanup = func() {
		configCleanup()
		os.Remove(scriptPath)
	}
	return cleanup, nil
}

// writeHookScript writes the script content to a temporary file and makes
// it executable. Returns the path to the created file.
func writeHookScript(script string) (string, error) {
	f, err := os.CreateTemp("", "herdingllamas-hook-*.sh")
	if err != nil {
		return "", fmt.Errorf("creating temp script file: %w", err)
	}

	path := f.Name()

	if _, err := f.WriteString(script); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("writing script to %s: %w", path, err)
	}

	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("closing script file %s: %w", path, err)
	}

	if err := os.Chmod(path, 0o755); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("making script executable %s: %w", path, err)
	}

	return path, nil
}

// configureClaudeStopHook adds a Stop hook to the project-level Claude
// settings.json that invokes the given script. Returns a cleanup function
// that removes the hook entry from settings.
func configureClaudeStopHook(workDir string, scriptPath string) (func(), error) {
	settings, err := claudeconfig.LoadProjectSettings(workDir)
	if err != nil {
		return nil, fmt.Errorf("loading Claude project settings: %w", err)
	}

	hook := claudeconfig.Hook{
		Matcher: hookMatcher,
		Hooks: []claudeconfig.HookEntry{
			{
				Type:    "command",
				Command: scriptPath,
				Timeout: 10,
			},
		},
	}

	settings.AddHook(claudeconfig.HookStop, hook)

	if err := claudeconfig.SaveProjectSettings(workDir, settings); err != nil {
		return nil, fmt.Errorf("saving Claude project settings: %w", err)
	}

	cleanup := func() {
		s, err := claudeconfig.LoadProjectSettings(workDir)
		if err != nil {
			return
		}
		if s.RemoveHook(claudeconfig.HookStop, hookMatcher) {
			claudeconfig.SaveProjectSettings(workDir, s)
		}
	}

	return cleanup, nil
}

// configureCodexStopHook writes a hooks.json file to {workDir}/.codex/
// that invokes the given script on Stop events. Returns a cleanup function
// that removes the hooks.json file.
func configureCodexStopHook(workDir string, scriptPath string) (func(), error) {
	codexDir := filepath.Join(workDir, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating .codex directory: %w", err)
	}

	hooksPath := filepath.Join(codexDir, "hooks.json")

	config := codexcontract.HookConfig{
		Hooks: map[string][]codexcontract.HookMatcher{
			string(codexcontract.HookStop): {
				{
					Matcher: hookMatcher,
					Hooks: []codexcontract.HookEntry{
						{
							Type:    "command",
							Command: scriptPath,
							Timeout: 10,
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling Codex hook config: %w", err)
	}

	if err := os.WriteFile(hooksPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("writing Codex hooks.json: %w", err)
	}

	cleanup := func() {
		os.Remove(hooksPath)
	}

	return cleanup, nil
}
