package integration

import (
	"strconv"
	"strings"
)

const (
	integrationVersionMarker       = "TEND_INTEGRATION_VERSION="
	legacyIntegrationVersionMarker = "HERDR_INTEGRATION_VERSION="
)

// ReadInstalledVersion parses the integration version marker from a hook asset.
// tend assets use TEND_INTEGRATION_VERSION=; HERDR_INTEGRATION_VERSION= is
// still recognized so an old herdr install is reported as outdated rather than
// missing.
func ReadInstalledVersion(content []byte) (uint32, bool) {
	for _, line := range strings.Split(string(content), "\n") {
		markerLine := strings.TrimSpace(line)
		markerLine = strings.TrimLeft(markerLine, "/")
		markerLine = strings.TrimLeft(markerLine, "#")
		markerLine = strings.TrimSpace(markerLine)

		for _, marker := range []string{integrationVersionMarker, legacyIntegrationVersionMarker} {
			value, ok := strings.CutPrefix(markerLine, marker)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			parsed, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				continue
			}
			return uint32(parsed), true
		}
	}
	return 0, false
}
