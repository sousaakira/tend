package integration

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed assets/omp/tend-agent-state.ts
var ompExtensionAsset string

// OmpExtensionInstallName is the install filename. The asset on disk is
// tend-agent-state.ts; herdr installed as herdr-omp-agent-state.ts so OMP and
// Pi never share a leaf name when both land in one extensions directory.
const OmpExtensionInstallName = "tend-omp-agent-state.ts"

// OmpIntegrationVersion is stamped in the extension header; bump when the asset
// changes in a way that needs reinstall.
const OmpIntegrationVersion = 10

// OmpInstallPaths are the files InstallOmp wrote or updated.
type OmpInstallPaths struct {
	ExtensionPath            string
	RemovedLegacyPiExtension bool
}

// OmpUninstallResult reports what UninstallOmp removed.
type OmpUninstallResult struct {
	ExtensionPath    string
	RemovedExtension bool
}

// InstallOmp copies the embedded extension into OMP's extensions directory.
// Refuses when Pi and OMP resolve to the same directory (shared
// $PI_CODING_AGENT_DIR), and removes a leftover Pi extension that was
// previously written into the OMP dir under Pi's install name.
func InstallOmp() (OmpInstallPaths, error) {
	dir, err := OmpExtensionDir()
	if err != nil {
		return OmpInstallPaths{}, err
	}
	piDir, err := PiExtensionDir()
	if err != nil {
		return OmpInstallPaths{}, err
	}
	if dir == piDir {
		return OmpInstallPaths{}, fmt.Errorf(
			"Pi and OMP resolve to the same extension directory at %s; configure separate agent directories before installing OMP",
			dir,
		)
	}
	if err := ensureExtensionDir(dir, "omp"); err != nil {
		return OmpInstallPaths{}, err
	}

	removedLegacy, err := removeLegacyPiExtensionFromOmpDir(dir)
	if err != nil {
		return OmpInstallPaths{}, err
	}
	extensionPath := filepath.Join(dir, OmpExtensionInstallName)
	if err := os.WriteFile(extensionPath, []byte(ompExtensionAsset), 0o644); err != nil {
		return OmpInstallPaths{}, err
	}
	return OmpInstallPaths{
		ExtensionPath:            extensionPath,
		RemovedLegacyPiExtension: removedLegacy,
	}, nil
}

// UninstallOmp removes the tend OMP extension when present.
func UninstallOmp() (OmpUninstallResult, error) {
	dir, err := OmpExtensionDir()
	if err != nil {
		return OmpUninstallResult{}, err
	}
	extensionPath := filepath.Join(dir, OmpExtensionInstallName)
	removed, err := RemoveFileIfExists(extensionPath)
	if err != nil {
		return OmpUninstallResult{ExtensionPath: extensionPath}, err
	}
	return OmpUninstallResult{
		ExtensionPath:    extensionPath,
		RemovedExtension: removed,
	}, nil
}

// removeLegacyPiExtensionFromOmpDir deletes a Pi-managed file that was wrongly
// installed into the OMP extensions directory. Only removes when the content
// carries tend/herdr's Pi integration id — a user file with the same name stays.
func removeLegacyPiExtensionFromOmpDir(dir string) (bool, error) {
	legacyPath := filepath.Join(dir, PiExtensionInstallName)
	info, err := os.Stat(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}
	content, err := os.ReadFile(legacyPath)
	if err != nil {
		return false, err
	}
	text := string(content)
	if strings.Contains(text, "TEND_INTEGRATION_ID=pi") ||
		strings.Contains(text, "HERDR_INTEGRATION_ID=pi") {
		if err := os.Remove(legacyPath); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}
