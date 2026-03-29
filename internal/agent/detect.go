package agent

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// ProviderStatus reports whether a provider's CLI binary is installed and
// authenticated. Returned by CheckProvider.
type ProviderStatus struct {
	Provider      Provider
	Installed     bool
	Authenticated bool
	Error         error // set when Installed or Authenticated is false
}

// CheckProvider verifies that a provider's CLI binary exists in PATH and is
// authenticated. The auth check runs the provider's status subcommand with a
// short timeout so it doesn't block startup.
func CheckProvider(ctx context.Context, p Provider) ProviderStatus {
	status := ProviderStatus{Provider: p}

	binary := string(p)
	_, err := exec.LookPath(binary)
	if err != nil {
		status.Error = fmt.Errorf("%s CLI not found in PATH", binary)
		return status
	}
	status.Installed = true

	authCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch p {
	case ProviderClaude:
		cmd = exec.CommandContext(authCtx, binary, "auth", "status")
	case ProviderCodex:
		cmd = exec.CommandContext(authCtx, binary, "login", "status")
	default:
		status.Error = fmt.Errorf("unknown provider: %s", binary)
		return status
	}

	if err := cmd.Run(); err != nil {
		status.Error = fmt.Errorf("%s is installed but not authenticated (run: %s auth login)", binary, binary)
		return status
	}
	status.Authenticated = true
	return status
}

// DetectProviders returns all providers that are both installed and
// authenticated, in preference order (claude first, then codex).
func DetectProviders(ctx context.Context) []Provider {
	var available []Provider
	for _, p := range []Provider{ProviderClaude, ProviderCodex} {
		s := CheckProvider(ctx, p)
		if s.Installed && s.Authenticated {
			available = append(available, p)
		}
	}
	return available
}
