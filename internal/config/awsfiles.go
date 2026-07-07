package config

import (
	"os"
	"path/filepath"
)

// AWSFiles carries the resolved AWS shared config and credentials file
// paths. It is resolved once at startup (cmd/root.go) and threaded through
// the TUI options so the profile picker's INI loader and every SDK client
// read the same files. Empty fields mean "no home dir could be resolved";
// consumers then fall back to the SDK's own defaults.
type AWSFiles struct {
	ConfigFile      string
	CredentialsFile string
}

// ResolveAWSFiles resolves both shared-file paths with the precedence
// explicit flag > environment variable > SDK default (~/.aws/...).
// It never checks that the files exist — a missing default/env file keeps
// today's behavior (empty profile list, SDK defaults); missing files named
// by an explicit flag are rejected at startup by cmd/root.go.
func ResolveAWSFiles(configFlag, credentialsFlag string) AWSFiles {
	return AWSFiles{
		ConfigFile:      resolveAWSFile(configFlag, "AWS_CONFIG_FILE", "config"),
		CredentialsFile: resolveAWSFile(credentialsFlag, "AWS_SHARED_CREDENTIALS_FILE", "credentials"),
	}
}

func resolveAWSFile(flagPath, envVar, base string) string {
	if flagPath != "" {
		return flagPath
	}
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".aws", base)
}
