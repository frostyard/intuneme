//go:build !linux

package nshelper

func MachineMain(args []string) int { return Main(args) }
