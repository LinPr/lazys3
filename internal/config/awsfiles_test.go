package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAWSFilesPrecedence(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}

	// Default: no flags, no env → ~/.aws/{config,credentials}.
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	got := ResolveAWSFiles("", "")
	if want := filepath.Join(home, ".aws", "config"); got.ConfigFile != want {
		t.Errorf("default ConfigFile = %q, want %q", got.ConfigFile, want)
	}
	if want := filepath.Join(home, ".aws", "credentials"); got.CredentialsFile != want {
		t.Errorf("default CredentialsFile = %q, want %q", got.CredentialsFile, want)
	}

	// Env beats default.
	t.Setenv("AWS_CONFIG_FILE", "/env/config")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/env/credentials")
	got = ResolveAWSFiles("", "")
	if got.ConfigFile != "/env/config" || got.CredentialsFile != "/env/credentials" {
		t.Errorf("env should beat default, got %+v", got)
	}

	// Flag beats env.
	got = ResolveAWSFiles("/flag/config", "/flag/credentials")
	if got.ConfigFile != "/flag/config" || got.CredentialsFile != "/flag/credentials" {
		t.Errorf("flag should beat env, got %+v", got)
	}

	// The two files resolve independently (only one flag given).
	got = ResolveAWSFiles("/flag/config", "")
	if got.ConfigFile != "/flag/config" || got.CredentialsFile != "/env/credentials" {
		t.Errorf("per-file resolution broken, got %+v", got)
	}
}
