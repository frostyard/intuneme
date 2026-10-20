package nshelper

import "fmt"

const (
	// MachineHelperPath is the root-owned helper authorized to stop the
	// configured machine and to bind the MCP directory read-only into it.
	MachineHelperPath = Dir + "/machine-exec"
	// MCPMountDir is the fixed container path for the host MCP binary directory.
	MCPMountDir = "/opt/intuneme-mcp"
)

// MachineHelperScript renders the small root-owned helper used for the two
// machinectl operations the CLI needs without a password. Both operations are
// fixed: stop the baked machine, or bind a user-owned directory read-only at a
// fixed container path. The caller cannot choose the machine, the destination,
// or make the bind writable. This lets the polkit rule stop granting broad
// org.freedesktop.machine1.manage-machines.
func MachineHelperScript(machine string, uid int) string {
	return fmt.Sprintf(`#!/bin/sh
# Installed by intuneme. The only machinectl operations allowed without a password.
set -eu
PATH=/run/current-system/sw/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

machine='%s'
uid='%d'

case "${1:-}" in
  stop)
    [ "$#" -eq 1 ] || exit 64
    exec machinectl poweroff "$machine"
    ;;
  bind)
    [ "$#" -eq 2 ] || exit 64
    dir=$2
    case "$dir" in
      /*) ;;
      *) echo "machine-exec: bind path must be absolute" >&2; exit 64 ;;
    esac
    [ -d "$dir" ] || { echo "machine-exec: not a directory: $dir" >&2; exit 1; }
    owner=$(stat -c '%%u' -- "$dir") || exit 1
    [ "$owner" = "$uid" ] || { echo "machine-exec: bind source must be owned by uid $uid" >&2; exit 1; }
    exec machinectl bind --read-only --mkdir "$machine" "$dir" /opt/intuneme-mcp
    ;;
  *)
    echo "usage: machine-exec stop|bind <directory>" >&2
    exit 64
    ;;
esac
`, machine, uid)
}
