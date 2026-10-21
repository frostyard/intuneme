package nshelper

import "path/filepath"

const (
	MachineHelperPath = Dir + "/machine-exec"
	MCPMountDir       = "/opt/intuneme-mcp"
)

func IsMachineHelperName(argv0 string) bool { return filepath.Base(argv0) == "machine-exec" }
