package backend

import (
	"regexp"
	"strings"
)

// Multipass says `instance "name" does not exist` (straight quotes) when the
// VM is already gone. Matching that phrasing — not a generic "does not exist"
// — avoids treating a real delete failure (missing disk file, etc.) as success.
var absentInstanceRe = regexp.MustCompile(`(?i)instance ["'][^"']+["'] does not exist`)

// IsAbsentInstance reports whether err means the hypervisor no longer has this
// VM. Destroy uses that to forget a leftover registry row instead of leaving
// a ghost that can never be deleted.
func IsAbsentInstance(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "already deleted") {
		return true
	}
	return absentInstanceRe.MatchString(err.Error())
}
