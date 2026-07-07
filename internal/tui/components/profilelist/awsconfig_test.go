package profilelist

import (
	"os"
	"path/filepath"
	"testing"

	appcfg "github.com/LinPr/lazys3/internal/config"
)

// writeFixtures writes a shared config + credentials pair into a temp dir
// and returns their paths. "shared" appears in both files to exercise the
// dedupe path.
func writeFixtures(t *testing.T) (configPath, credentialsPath string) {
	t.Helper()
	dir := t.TempDir()
	configPath = filepath.Join(dir, "aws-config")
	credentialsPath = filepath.Join(dir, "aws-credentials")
	config := `[profile alpha]
region = us-west-2
endpoint_url = https://oss.example.com

[profile shared]
region = eu-west-1
`
	credentials := `[shared]
aws_access_key_id = AKIAEXAMPLE
aws_secret_access_key = secretexample

[credsonly]
aws_access_key_id = AKIAEXAMPLE2
aws_secret_access_key = secretexample2
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath, credentialsPath
}

func TestLoadAwsConfigFromCustomFiles(t *testing.T) {
	configPath, credentialsPath := writeFixtures(t)
	configs, err := LoadAwsConfig(appcfg.AWSFiles{ConfigFile: configPath, CredentialsFile: credentialsPath})
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]int{}
	for _, c := range configs {
		byName[c.Profile]++
	}
	for _, want := range []string{"alpha", "shared", "credsonly"} {
		if byName[want] != 1 {
			t.Errorf("profile %q listed %d times, want 1 (got %v)", want, byName[want], byName)
		}
	}

	for _, c := range configs {
		if c.Profile != "alpha" {
			continue
		}
		if c.Region != "us-west-2" {
			t.Errorf("alpha region = %q", c.Region)
		}
		if c.BaseEndpoint != "https://oss.example.com" {
			t.Errorf("alpha endpoint = %q", c.BaseEndpoint)
		}
	}
}

func TestReadAwsConfigProfileListCmdHonorsEnvViaResolver(t *testing.T) {
	configPath, credentialsPath := writeFixtures(t)
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)

	// NewModel resolves flag > env > default; with no flags the env vars
	// must win (they used to be ignored by the INI loader).
	m := NewModel()
	msg := m.Init()()
	result, ok := msg.(ReadAwsConfigResult)
	if !ok {
		t.Fatalf("Init cmd returned %T", msg)
	}
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	names := map[string]bool{}
	for _, p := range result.Profiles {
		names[p.Title()] = true
	}
	for _, want := range []string{"alpha", "shared", "credsonly"} {
		if !names[want] {
			t.Errorf("profile %q missing from env-resolved listing: %v", want, names)
		}
	}
}
