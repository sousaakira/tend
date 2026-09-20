package integration

import (
	"os"
)

// RemoveFileIfExists deletes path. Missing is not an error; returns whether a
// file was removed.
func RemoveFileIfExists(path string) (bool, error) {
	err := os.Remove(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// RemoveDirAllIfExists deletes path recursively. Missing is not an error;
// returns whether a directory was removed.
func RemoveDirAllIfExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return RemoveFileIfExists(path)
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

// MakeExecutable sets mode 0755 on Unix so Claude can run the hook.
func MakeExecutable(path string) error {
	return os.Chmod(path, 0o755)
}
