// Package profilelist renders the AWS shared-config profile picker and
// loads profiles from the resolved shared config/credentials files
// (--aws-config/--aws-credentials > AWS_CONFIG_FILE/
// AWS_SHARED_CREDENTIALS_FILE > ~/.aws/config and ~/.aws/credentials).
package profilelist

import (
	"errors"
	"fmt"

	"github.com/LinPr/lazys3/internal/ini"
)

const (
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

	// S3 Disable Multi-Region AccessPoints
	s3DisableMultiRegionAccessPointsKey = `s3_disable_multiregion_access_points`

	useFIPSEndpointKey = "use_fips_endpoint"

	defaultsModeKey = "defaults_mode"

	// Retry options
	retryMaxAttemptsKey = "max_attempts"
	retryModeKey        = "retry_mode"

	caBundleKey = "ca_bundle"

	authSchemePreferenceKey = "auth_scheme_preference"
)

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
			dstSection.UpdateValue(accessKeyIDKey, v) //nolint:errcheck // best-effort merge; errors surfaced via Logs

			// update secret key
			v, err = ini.NewStringValue(secretKey)
			if err != nil {
				return fmt.Errorf("error merging secret key, %w", err)
			}
			dstSection.UpdateValue(secretAccessKey, v) //nolint:errcheck // best-effort merge; errors surfaced via Logs

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

		dstSection.UpdateValue(key, val) //nolint:errcheck // best-effort merge; errors surfaced via Logs
		dstSection.UpdateSourceFile(key, srcSection.SourceFile[key])
	}
	return nil
}
