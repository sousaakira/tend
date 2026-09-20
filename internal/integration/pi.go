package integration

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed assets/pi/tend-agent-state.ts
var piExtensionAsset string

// PiExtensionInstallName is the filename written under the Pi extensions dir.
const PiExtensionInstallName = "tend-agent-state.ts"

// PiIntegrationVersion is stamped in the extension header; bump when the asset
// changes in a way that needs reinstall.
const PiIntegrationVersion = 9

// PiUninstallResult reports what UninstallPi removed.
type PiUninstallResult struct {
	ExtensionPath    string
	RemovedExtension bool
}

// InstallPi copies the embedded extension into Pi's extensions directory.
func InstallPi() (string, error) {
	dir, err := PiExtensionDir()
	if err != nil {
		return "", err
	}
	if err := ensureExtensionDir(dir, "pi"); err != nil {
		return "", err
	}
	path := filepath.Join(dir, PiExtensionInstallName)
	if err := os.WriteFile(path, []byte(piExtensionAsset), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// UninstallPi removes the tend Pi extension when present.
func UninstallPi() (PiUninstallResult, error) {
	dir, err := PiExtensionDir()
	if err != nil {
		return PiUninstallResult{}, err
	}
	extensionPath := filepath.Join(dir, PiExtensionInstallName)
	removed, err := RemoveFileIfExists(extensionPath)
	if err != nil {
		return PiUninstallResult{ExtensionPath: extensionPath}, err
	}
	return PiUninstallResult{
		ExtensionPath:    extensionPath,
		RemovedExtension: removed,
	}, nil
}

// ensureExtensionDir creates dir when its parent (the agent dir) already
// exists; otherwise tells the user to install the agent first. Matches herdr:
// we do not mkdir -p from home, only fill in a missing extensions/ leaf.
func ensureExtensionDir(dir, agent string) error {
	info, err := os.Stat(dir)
	if err == nil {
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("%s extension directory not found at %s. install %s first", agent, dir, agent)
	}
	if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(dir)
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return fmt.Errorf("%s extension directory not found at %s. install %s first", agent, dir, agent)
	}
	return os.MkdirAll(dir, 0o755)
}
