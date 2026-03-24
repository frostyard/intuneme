package sudo

import (
	"fmt"
	"os"

	"github.com/frostyard/intuneme/internal/runner"
)

// WriteFile writes data to path via a temp file + sudo install.
// This avoids needing root to create the temp file while still installing
// the final file with correct ownership (root) and permissions.
func WriteFile(r runner.Runner, path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp("", "intuneme-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Close()
	_, err = r.Run("sudo", "install", "-m", fmt.Sprintf("%04o", perm), tmp.Name(), path)
	return err
}
