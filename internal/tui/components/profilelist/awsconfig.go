package profilelist

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/LinPr/lazys3/internal/ini"
)

const (
	// Prefix to use for filtering profiles. The profile prefix should only
	// exist in the shared config file, not the credentials file.
	profilePrefix = `profile `

	// Prefix to be used for SSO sections. These are supposed to only exist in
	// the shared config file, not the credentials file.
	ssoSectionPrefix = `sso-session `

	// Prefix for services section. It is referenced in profile via the services
	// parameter to configure clients for service-specific parameters.
	servicesPrefix = `services `

	// string equivalent for boolean
	endpointDiscoveryDisabled = `false`
	endpointDiscoveryEnabled  = `true`
	endpointDiscoveryAuto     = `auto`

	// Static Credentials group
	accessKeyIDKey  = `aws_access_key_id`     // group required
	secretAccessKey = `aws_secret_access_key` // group required
	sessionTokenKey = `aws_session_token`     // optional

	// Assume Role Credentials group
	roleArnKey             = `role_arn`          // group required
	sourceProfileKey       = `source_profile`    // group required
	credentialSourceKey    = `credential_source` // group required (or source_profile)
	externalIDKey          = `external_id`       // optional
	mfaSerialKey           = `mfa_serial`        // optional
	roleSessionNameKey     = `role_session_name` // optional
	roleDurationSecondsKey = "duration_seconds"  // optional

	// AWS Single Sign-On (AWS SSO) group
	ssoSessionNameKey = "sso_session"

	ssoRegionKey   = "sso_region"
	ssoStartURLKey = "sso_start_url"

	ssoAccountIDKey = "sso_account_id"
	ssoRoleNameKey  = "sso_role_name"

	// Additional Config fields
	regionKey = `region`

	// endpoint discovery group
	enableEndpointDiscoveryKey = `endpoint_discovery_enabled` // optional

	// External Credential process
	credentialProcessKey = `credential_process` // optional

	// Web Identity Token File
	webIdentityTokenFileKey = `web_identity_token_file` // optional

	// S3 ARN Region Usage
	s3UseARNRegionKey = "s3_use_arn_region"

	ec2MetadataServiceEndpointModeKey = "ec2_metadata_service_endpoint_mode"

	ec2MetadataServiceEndpointKey = "ec2_metadata_service_endpoint"

	ec2MetadataV1DisabledKey = "ec2_metadata_v1_disabled"

	// Use DualStack Endpoint Resolution
	useDualStackEndpoint = "use_dualstack_endpoint"

	// DefaultSharedConfigProfile is the default profile to be used when
	// loading configuration from the config files if another profile name
	// is not provided.
	DefaultSharedConfigProfile = `default`

	// S3 Disable Multi-Region AccessPoints
	s3DisableMultiRegionAccessPointsKey = `s3_disable_multiregion_access_points`

	useFIPSEndpointKey = "use_fips_endpoint"

	defaultsModeKey = "defaults_mode"

	// Retry options
	retryMaxAttemptsKey = "max_attempts"
	retryModeKey        = "retry_mode"

	caBundleKey = "ca_bundle"

	sdkAppID = "sdk_ua_app_id"

	ignoreConfiguredEndpoints = "ignore_configured_endpoint_urls"

	endpointURL = "endpoint_url"

	servicesSectionKey = "services"

	disableRequestCompression      = "disable_request_compression"
	requestMinCompressionSizeBytes = "request_min_compression_size_bytes"

	s3DisableExpressSessionAuthKey = "s3_disable_express_session_auth"

	accountIDKey          = "aws_account_id"
	accountIDEndpointMode = "account_id_endpoint_mode"

	requestChecksumCalculationKey = "request_checksum_calculation"
	responseChecksumValidationKey = "response_checksum_validation"
	checksumWhenSupported         = "when_supported"
	checksumWhenRequired          = "when_required"

	authSchemePreferenceKey = "auth_scheme_preference"
)

// defaultSharedConfigProfile allows for swapping the default profile for testing
var defaultSharedConfigProfile = "default"

// DefaultSharedCredentialsFilename returns the SDK's default file path
// for the shared credentials file.
//
// Builds the shared config file path based on the OS's platform.
//
//   - Linux/Unix: $HOME/.aws/credentials
//   - Windows: %USERPROFILE%\.aws\credentials
func DefaultSharedCredentialsFilename() string {
	return filepath.Join(SharedCredentialsFilename())
}

// DefaultSharedConfigFilename returns the SDK's default file path for
// the shared config file.
//
// Builds the shared config file path based on the OS's platform.
//
//   - Linux/Unix: $HOME/.aws/config
//   - Windows: %USERPROFILE%\.aws\config
func DefaultSharedConfigFilename() string {
	return filepath.Join(SharedConfigFilename())
}

// DefaultSharedConfigFiles is a slice of the default shared config files that
// the will be used in order to load the SharedConfig.
var DefaultSharedConfigFiles = []string{
	DefaultSharedConfigFilename(),
}

// DefaultSharedCredentialsFiles is a slice of the default shared credentials
// files that the will be used in order to load the SharedConfig.
var DefaultSharedCredentialsFiles = []string{
	DefaultSharedCredentialsFilename(),
}

// SharedCredentialsFilename returns the SDK's default file path
// for the shared credentials file.
//
// Builds the shared config file path based on the OS's platform.
//
//   - Linux/Unix: $HOME/.aws/credentials
//   - Windows: %USERPROFILE%\.aws\credentials
func SharedCredentialsFilename() string {
	return filepath.Join(UserHomeDir(), ".aws", "credentials")
}

// SharedConfigFilename returns the SDK's default file path for
// the shared config file.
//
// Builds the shared config file path based on the OS's platform.
//
//   - Linux/Unix: $HOME/.aws/config
//   - Windows: %USERPROFILE%\.aws\config
func SharedConfigFilename() string {
	return filepath.Join(UserHomeDir(), ".aws", "config")
}

// SharedConfigLoadError is an error for the shared config file failed to load.
type SharedConfigLoadError struct {
	Filename string
	Err      error
}

// Unwrap returns the underlying error that caused the failure.
func (e SharedConfigLoadError) Unwrap() error {
	return e.Err
}

func (e SharedConfigLoadError) Error() string {
	return fmt.Sprintf("failed to load shared config file, %s, %v", e.Filename, e.Err)
}

// UserHomeDir returns the home directory for the user the process is
// running under.
func UserHomeDir() string {
	// Ignore errors since we only care about Windows and *nix.
	home, _ := os.UserHomeDir()

	if len(home) > 0 {
		return home
	}

	currUser, _ := user.Current()
	if currUser != nil {
		home = currUser.HomeDir
	}

	return home
}

// LoadSharedConfigOptions struct contains optional values that can be used to load the config.
type LoadSharedConfigOptions struct {

	// CredentialsFiles are the shared credentials files
	CredentialsFiles []string

	// ConfigFiles are the shared config files
	ConfigFiles []string

	// Logger is the logger used to log shared config behavior
	// Logger logging.Logger
}

func loadIniFiles(filenames []string) (ini.Sections, error) {
	mergedSections := ini.NewSections()

	for _, filename := range filenames {
		sections, err := ini.OpenFile(filename)
		var v *ini.UnableToReadFile
		if ok := errors.As(err, &v); ok {
			// Skip files which can't be opened and read for whatever reason.
			// We treat such files as empty, and do not fall back to other locations.
			continue
		} else if err != nil {
			return ini.Sections{}, SharedConfigLoadError{Filename: filename, Err: err}
		}

		// mergeSections into mergedSections
		err = mergeSections(&mergedSections, sections)
		if err != nil {
			return ini.Sections{}, SharedConfigLoadError{Filename: filename, Err: err}
		}
	}

	return mergedSections, nil
}

func processConfigSections(ctx context.Context, sections *ini.Sections) error {
	skipSections := map[string]struct{}{}

	for _, section := range sections.List() {
		if _, ok := skipSections[section]; ok {
			continue
		}

		// drop sections from config file that do not have expected prefixes.
		switch {
		case strings.HasPrefix(section, profilePrefix):
			// Rename sections to remove "profile " prefixing to match with
			// credentials file. If default is already present, it will be
			// dropped.
			newName, err := renameProfileSection(section, sections)
			if err != nil {
				return fmt.Errorf("failed to rename profile section, %w", err)
			}
			skipSections[newName] = struct{}{}

		case strings.HasPrefix(section, ssoSectionPrefix):
		case strings.HasPrefix(section, servicesPrefix):
		case strings.EqualFold(section, "default"):
		default:
			// drop this section, as invalid profile name
			sections.DeleteSection(section)

			// if logger != nil {
			// 	logger.Logf(logging.Debug, "A profile defined with name `%v` is ignored. "+
			// 		"For use within a shared configuration file, "+
			// 		"a non-default profile must have `profile ` "+
			// 		"prefixed to the profile name.",
			// 		section,
			// 	)
			// }
		}
	}
	return nil
}

func processCredentialsSections(ctx context.Context, sections *ini.Sections) error {
	for _, section := range sections.List() {
		// drop profiles with prefix for credential files
		if strings.HasPrefix(section, profilePrefix) {
			// drop this section, as invalid profile name
			sections.DeleteSection(section)

			// if logger != nil {
			// logger.Logf(logging.Debug,
			// 	"The profile defined with name `%v` is ignored. A profile with the `profile ` prefix is invalid "+
			// 		"for the shared credentials file.\n",
			// 	section,
			// )
			// }
		}
	}
	return nil
}

func renameProfileSection(section string, sections *ini.Sections) (string, error) {
	v, ok := sections.GetSection(section)
	if !ok {
		return "", fmt.Errorf("error processing profiles within the shared configuration files")
	}

	// delete section with profile as prefix
	sections.DeleteSection(section)

	// set the value to non-prefixed name in sections.
	section = strings.TrimPrefix(section, profilePrefix)
	if sections.HasSection(section) {
		oldSection, _ := sections.GetSection(section)
		v.Logs = append(v.Logs,
			fmt.Sprintf("A non-default profile not prefixed with `profile ` found in %s, "+
				"overriding non-default profile from %s",
				v.SourceFile, oldSection.SourceFile))
		sections.DeleteSection(section)
	}

	// assign non-prefixed name to section
	v.Name = section
	sections.SetSection(section, v)

	return section, nil
}

// mergeSections merges source section properties into destination section properties
func mergeSections(dst *ini.Sections, src ini.Sections) error {
	for _, sectionName := range src.List() {
		srcSection, _ := src.GetSection(sectionName)

		if (!srcSection.Has(accessKeyIDKey) && srcSection.Has(secretAccessKey)) ||
			(srcSection.Has(accessKeyIDKey) && !srcSection.Has(secretAccessKey)) {
			srcSection.Errors = append(srcSection.Errors,
				fmt.Errorf("partial credentials found for profile %v", sectionName))
		}

		if !dst.HasSection(sectionName) {
			dst.SetSection(sectionName, srcSection)
			continue
		}

		// merge with destination srcSection
		dstSection, _ := dst.GetSection(sectionName)

		// errors should be overriden if any
		dstSection.Errors = srcSection.Errors

		// Access key id update
		if srcSection.Has(accessKeyIDKey) && srcSection.Has(secretAccessKey) {
			accessKey := srcSection.String(accessKeyIDKey)
			secretKey := srcSection.String(secretAccessKey)

			if dstSection.Has(accessKeyIDKey) {
				dstSection.Logs = append(dstSection.Logs, newMergeKeyLogMessage(sectionName, accessKeyIDKey,
					dstSection.SourceFile[accessKeyIDKey], srcSection.SourceFile[accessKeyIDKey]))
			}

			// update access key
			v, err := ini.NewStringValue(accessKey)
			if err != nil {
				return fmt.Errorf("error merging access key, %w", err)
			}
			dstSection.UpdateValue(accessKeyIDKey, v)

			// update secret key
			v, err = ini.NewStringValue(secretKey)
			if err != nil {
				return fmt.Errorf("error merging secret key, %w", err)
			}
			dstSection.UpdateValue(secretAccessKey, v)

			// update session token
			if err = mergeStringKey(&srcSection, &dstSection, sectionName, sessionTokenKey); err != nil {
				return err
			}

			// update source file to reflect where the static creds came from
			dstSection.UpdateSourceFile(accessKeyIDKey, srcSection.SourceFile[accessKeyIDKey])
			dstSection.UpdateSourceFile(secretAccessKey, srcSection.SourceFile[secretAccessKey])
		}

		stringKeys := []string{
			roleArnKey,
			sourceProfileKey,
			credentialSourceKey,
			externalIDKey,
			mfaSerialKey,
			roleSessionNameKey,
			regionKey,
			enableEndpointDiscoveryKey,
			credentialProcessKey,
			webIdentityTokenFileKey,
			s3UseARNRegionKey,
			s3DisableMultiRegionAccessPointsKey,
			ec2MetadataServiceEndpointModeKey,
			ec2MetadataServiceEndpointKey,
			ec2MetadataV1DisabledKey,
			useDualStackEndpoint,
			useFIPSEndpointKey,
			defaultsModeKey,
			retryModeKey,
			caBundleKey,
			roleDurationSecondsKey,
			retryMaxAttemptsKey,

			ssoSessionNameKey,
			ssoAccountIDKey,
			ssoRegionKey,
			ssoRoleNameKey,
			ssoStartURLKey,

			authSchemePreferenceKey,
		}
		for i := range stringKeys {
			if err := mergeStringKey(&srcSection, &dstSection, sectionName, stringKeys[i]); err != nil {
				return err
			}
		}

		// set srcSection on dst srcSection
		*dst = dst.SetSection(sectionName, dstSection)
	}

	return nil
}

func newMergeKeyLogMessage(sectionName, key, dstSourceFile, srcSourceFile string) string {
	return fmt.Sprintf("For profile: %v, overriding %v value, defined in %v "+
		"with a %v value found in a duplicate profile defined at file %v. \n",
		sectionName, key, dstSourceFile, key, srcSourceFile)
}

func mergeStringKey(srcSection *ini.Section, dstSection *ini.Section, sectionName, key string) error {
	if srcSection.Has(key) {
		srcValue := srcSection.String(key)
		val, err := ini.NewStringValue(srcValue)
		if err != nil {
			return fmt.Errorf("error merging %s, %w", key, err)
		}

		if dstSection.Has(key) {
			dstSection.Logs = append(dstSection.Logs, newMergeKeyLogMessage(sectionName, key,
				dstSection.SourceFile[key], srcSection.SourceFile[key]))
		}

		dstSection.UpdateValue(key, val)
		dstSection.UpdateSourceFile(key, srcSection.SourceFile[key])
	}
	return nil
}
