// SPDX-License-Identifier: Apache-2.0

// Package dchook provides shared utilities for the dchook webhook system.
package dchook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	errSecretSymlink         = errors.New("secret file must not be a symlink")
	errSecretNotAbsolute     = errors.New("secret file must be an absolute path")
	errSecretForbiddenDir    = errors.New("secret file is in forbidden system directory")
	errSecretInsecurePerms   = errors.New("secret file has insecure permissions")
	errSecretInvalidFileType = errors.New("secret file must be regular file or named pipe")
)

// ReadSecretFileStrict reads and validates a secret file with strict security checks.
//
// Requires an absolute path and may not be a symlink (there may be symlinks in the path,
// but the last part must not be a symlink).
//
// On Unix-style platforms, the paths must not be pointing to forbidden ssystem
// directories or files (`/etc/shadow`, `/etc/passwd`, `/proc`, `/sys`, or `/dev` except
// `/dev/fd` for process substitution) and the file must be *either* a regular file with
// `0600` / `0400` permissions or a named pipe.
func ReadSecretFileStrict(path string) (string, error) {
	return readSecretFile(path, true)
}

// ReadSecretFileLax reads and validates a secret file with relaxed security checks.
//
// The file may be a relative path, but may not be a symlink (there may be symlinks in the
// path, but the last part must not be a symlink).
//
// On Unix-style platforms, the paths must not be pointing to forbidden ssystem
// directories or files (`/etc/shadow`, `/etc/passwd`, `/proc`, `/sys`, or `/dev` except
// `/dev/fd` for process substitution) and the file must be *either* a regular file with
// `0600` / `0400` permissions or a named pipe.
func ReadSecretFileLax(path string) (string, error) {
	return readSecretFile(path, false)
}

func readSecretFile(path string, requireAbsolute bool) (string, error) {
	validatedPath, err := validateSecretFile(path, requireAbsolute)
	if err != nil {
		return "", err
	}

	secretBytes, err := os.ReadFile(filepath.Clean(validatedPath))
	if err != nil {
		return "", fmt.Errorf("failed to read secret file %q: %w", path, err)
	}

	return strings.TrimSpace(string(secretBytes)), nil
}

func validateSecretFile(path string, requireAbsolute bool) (string, error) {
	// Check if the path is a symlink before resolving
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat secret file %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %q", errSecretSymlink, path)
	}

	// Windows-specific checks
	if runtime.GOOS == "windows" {
		if requireAbsolute && !filepath.IsAbs(path) {
			return "", fmt.Errorf("%w: %q", errSecretNotAbsolute, path)
		}
		return path, nil
	}

	// Unix-specific checks
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("invalid secret file path %q: %w", path, err)
	}

	if !filepath.IsAbs(resolvedPath) {
		if requireAbsolute {
			return "", fmt.Errorf("%w: %q", errSecretNotAbsolute, path)
		}
		// For relative paths, use the original path
		resolvedPath = path
	}

	// Prevent reading from sensitive system directories
	// Note: /dev/fd is allowed for process substitution (e.g., <(echo secret))
	forbiddenPrefixes := []string{"/etc/shadow", "/etc/passwd", "/proc", "/sys"}
	for _, prefix := range forbiddenPrefixes {
		if strings.HasPrefix(resolvedPath, prefix) {
			return "", fmt.Errorf("%w: %q", errSecretForbiddenDir, path)
		}
	}

	// Block /dev except /dev/fd (process substitution)
	if strings.HasPrefix(resolvedPath, "/dev/") && !strings.HasPrefix(resolvedPath, "/dev/fd/") {
		return "", fmt.Errorf("%w: %q", errSecretForbiddenDir, path)
	}

	// Check file type and permissions
	mode := info.Mode()

	if mode.IsRegular() {
		perm := mode.Perm()
		if perm != 0o600 && perm != 0o400 {
			return "", fmt.Errorf(
				"%w (must be 0600 or 0400): %q has %o",
				errSecretInsecurePerms,
				path,
				perm,
			)
		}
	} else if mode&os.ModeNamedPipe == 0 {
		return "", fmt.Errorf(
			"%w (got %s): %q",
			errSecretInvalidFileType,
			mode.String(),
			path,
		)
	}

	return resolvedPath, nil
}
