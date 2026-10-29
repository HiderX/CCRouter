package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"golang.org/x/text/unicode/norm"
)

// KeychainService is the Keychain item Claude Code uses for a config directory.
func KeychainService(profile string) string {
	if IsDefaultProfile(profile) {
		return "Claude Code-credentials"
	}
	hash := sha256.Sum256([]byte(norm.NFC.String(profile)))
	return fmt.Sprintf("Claude Code-credentials-%x", hash[:4])
}

// LoggedIn reports false only when no stored credentials exist for the profile. It looks up
// the Keychain item's attributes, not its secret, so it never prompts. Other failures, such
// as a locked Keychain, count as logged in rather than blocking a launch.
func LoggedIn(profile string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	e := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", KeychainService(profile)).Run()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 44 {
		return true
	}
	return hasCredentialsFile(profile)
}
