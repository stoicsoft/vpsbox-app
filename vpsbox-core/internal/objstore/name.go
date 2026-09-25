package objstore

import (
	"fmt"
	"net"
	"strings"
)

// ValidateBucketName applies the S3 bucket naming rules. The server enforces them
// too, but it answers with a generic XML fault — catching it here means the user
// gets told which rule they broke.
//
// Dots are rejected outright even though S3 allows them: a dotted bucket name
// breaks virtual-host style addressing over TLS (the wildcard cert covers one
// label) and is the single most common source of confusing S3 errors.
func ValidateBucketName(name string) error {
	if name == "" {
		return fmt.Errorf("bucket name is required")
	}
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("bucket name must be 3-63 characters, got %d", len(name))
	}
	if strings.Contains(name, ".") {
		return fmt.Errorf("bucket name %q cannot contain dots — they break subdomain-style addressing; use hyphens", name)
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("bucket name %q must be lowercase", name)
	}
	for _, r := range name {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit && r != '-' {
			return fmt.Errorf("bucket name %q may only contain lowercase letters, digits and hyphens", name)
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return fmt.Errorf("bucket name %q cannot start or end with a hyphen", name)
	}
	// S3 rejects names that parse as an IP address, because they would be
	// ambiguous with path-style addressing against a bare host.
	if net.ParseIP(name) != nil {
		return fmt.Errorf("bucket name %q cannot be an IP address", name)
	}
	for _, prefix := range []string{"xn--", "sthree-"} {
		if strings.HasPrefix(name, prefix) {
			return fmt.Errorf("bucket name %q cannot start with %q (reserved)", name, prefix)
		}
	}
	for _, suffix := range []string{"-s3alias", "--ol-s3"} {
		if strings.HasSuffix(name, suffix) {
			return fmt.Errorf("bucket name %q cannot end with %q (reserved)", name, suffix)
		}
	}
	return nil
}
