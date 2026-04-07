package udev

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/frostyard/intuneme/internal/nspawn"
	"github.com/frostyard/intuneme/internal/runner"
	"github.com/frostyard/intuneme/internal/sudo"
)

const (
	RulesFile      = "70-intuneme-yubikey.rules"
	VideoRulesFile = "70-intuneme-video.rules"
	RulesDir       = "/etc/udev/rules.d"
	ScriptDir      = "/usr/local/lib/intuneme"
	ScriptName     = "usb-hotplug"
	StateDir       = "/run/intuneme/devices"

	YubicoVendorID = "1050"
)

//go:embed 70-intuneme-yubikey.rules
var rulesTemplate string

//go:embed 70-intuneme-video.rules
var videoRulesTemplate string

//go:embed usb-hotplug.sh.tmpl
var scriptTemplate string

func scriptContent(machine string) string {
	return strings.ReplaceAll(scriptTemplate, "{{.Machine}}", machine)
}

// YubikeyDevice represents a detected Yubico USB security key.
type YubikeyDevice struct {
	// USBDevice is the USB device node path (e.g., /dev/bus/usb/003/009).
	USBDevice string
	// HIDRawDevices are associated hidraw device paths (e.g., /dev/hidraw3).
	HIDRawDevices []string
	// Product is the product ID (e.g., "0406").
	Product string
	// Name is a human-readable product name from sysfs.
	Name string
}

// Devices returns all device node paths that should be forwarded.
func (y YubikeyDevice) Devices() []string {
	devs := []string{y.USBDevice}
	devs = append(devs, y.HIDRawDevices...)
	return devs
}

// RulesPath returns the full path to the udev rules file.
func RulesPath() string {
	return filepath.Join(RulesDir, RulesFile)
}

// VideoRulesPath returns the full path to the video udev rules file.
func VideoRulesPath() string {
	return filepath.Join(RulesDir, VideoRulesFile)
}

// ScriptPath returns the full path to the helper script.
func ScriptPath() string {
	return filepath.Join(ScriptDir, ScriptName)
}

// Install writes the udev rule file and helper script, then reloads udev.
func Install(r runner.Runner, machineName string) error {
	// Create script directory.
	if _, err := r.Run("sudo", "mkdir", "-p", ScriptDir); err != nil {
		return fmt.Errorf("create script dir: %w", err)
	}

	// Write helper script.
	if err := sudo.WriteFile(r, ScriptPath(), []byte(scriptContent(machineName)), 0755); err != nil {
		return fmt.Errorf("install helper script: %w", err)
	}

	// Write udev rules.
	if err := sudo.WriteFile(r, RulesPath(), []byte(rulesTemplate), 0644); err != nil {
		return fmt.Errorf("install yubikey udev rule: %w", err)
	}
	if err := sudo.WriteFile(r, VideoRulesPath(), []byte(videoRulesTemplate), 0644); err != nil {
		return fmt.Errorf("install video udev rule: %w", err)
	}

	// Reload udev rules.
	if _, err := r.Run("sudo", "udevadm", "control", "--reload-rules"); err != nil {
		return fmt.Errorf("reload udev rules: %w", err)
	}

	return nil
}

// Remove deletes the udev rule file, helper script, and state directory.
// It is intentionally graceful: missing files and failed reloads are not errors.
func Remove(r runner.Runner) error {
	yubikeyRulesExisted := fileExists(RulesPath())
	videoRulesExisted := fileExists(VideoRulesPath())
	scriptExisted := fileExists(ScriptPath())

	if yubikeyRulesExisted {
		_, _ = r.Run("sudo", "rm", "-f", RulesPath())
	}
	if videoRulesExisted {
		_, _ = r.Run("sudo", "rm", "-f", VideoRulesPath())
	}
	if scriptExisted {
		_, _ = r.Run("sudo", "rm", "-f", ScriptPath())
	}

	// Clean up empty script directory (ignore errors — may not be empty).
	_, _ = r.Run("sudo", "rmdir", ScriptDir)

	// Clean up state directory.
	_, _ = r.Run("sudo", "rm", "-rf", StateDir)

	// Reload udev rules if we removed any rule file.
	if yubikeyRulesExisted || videoRulesExisted {
		_, _ = r.Run("sudo", "udevadm", "control", "--reload-rules")
	}

	return nil
}

// IsInstalled reports whether any udev rules files are installed.
func IsInstalled() bool {
	return fileExists(RulesPath()) || fileExists(VideoRulesPath())
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DetectYubikeys scans sysfs for currently plugged Yubico USB devices.
func DetectYubikeys() []YubikeyDevice {
	var devices []YubikeyDevice

	usbDevicesDir := "/sys/bus/usb/devices"
	entries, err := os.ReadDir(usbDevicesDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		devDir := filepath.Join(usbDevicesDir, e.Name())

		vendor, err := readSysfsAttr(devDir, "idVendor")
		if err != nil || vendor != YubicoVendorID {
			continue
		}

		product, _ := readSysfsAttr(devDir, "idProduct")
		name, _ := readSysfsAttr(devDir, "product")
		busnum, _ := readSysfsAttr(devDir, "busnum")
		devnum, _ := readSysfsAttr(devDir, "devnum")

		if busnum == "" || devnum == "" {
			continue
		}

		busNum, err := strconv.Atoi(busnum)
		if err != nil {
			continue
		}
		devNum, err := strconv.Atoi(devnum)
		if err != nil {
			continue
		}

		usbDevNode := usbDevNodePath(busNum, devNum)
		// Verify the device node exists.
		if !fileExists(usbDevNode) {
			continue
		}

		dev := YubikeyDevice{
			USBDevice: usbDevNode,
			Product:   product,
			Name:      name,
		}

		// Find associated hidraw devices.
		dev.HIDRawDevices = findHIDRawDevices(devDir)

		devices = append(devices, dev)
	}

	return devices
}

// usbDevNodePath returns the /dev/bus/usb device node path for the given bus and device numbers.
func usbDevNodePath(bus, dev int) string {
	return fmt.Sprintf("/dev/bus/usb/%03d/%03d", bus, dev)
}

// readSysfsAttr reads a single sysfs attribute file.
func readSysfsAttr(devDir, attr string) (string, error) {
	data, err := os.ReadFile(filepath.Join(devDir, attr))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// findHIDRawDevices finds hidraw device nodes associated with a USB device sysfs path.
func findHIDRawDevices(usbDevDir string) []string {
	// HID raw devices appear as subdirectories like:
	//   <usbdev>/<iface>/<hid-dev>/hidraw/hidrawN
	pattern := filepath.Join(usbDevDir, "*", "*", "hidraw", "hidraw*")
	matches, _ := filepath.Glob(pattern)

	var devices []string
	for _, m := range matches {
		name := filepath.Base(m)
		devNode := "/dev/" + name
		if fileExists(devNode) {
			devices = append(devices, devNode)
		}
	}
	return devices
}

// ForwardDevice creates a device node inside the running container using nsenter.
// For hidraw devices, it also adjusts the cgroup device allow list.
func ForwardDevice(r runner.Runner, machine, devnode string) error {
	pid, err := nspawn.LeaderPID(r, machine)
	if err != nil {
		return err
	}
	unit, err := nspawn.MachineUnit(r, machine)
	if err != nil {
		return err
	}

	// Get major:minor of the device.
	out, err := r.Run("stat", "-c", "0x%t 0x%T", devnode)
	if err != nil {
		return fmt.Errorf("stat %s: %w", devnode, err)
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) != 2 {
		return fmt.Errorf("unexpected stat output for %s: %q", devnode, string(out))
	}
	major, minor := parts[0], parts[1]

	// Allow the device in the container's cgroup. DevicePolicy=auto preserves
	// the existing nspawn device policy and adds our device on top.
	if _, err := r.Run("sudo", "systemctl", "set-property",
		unit,
		"DevicePolicy=auto",
		fmt.Sprintf("DeviceAllow=%s rwm", devnode)); err != nil {
		return fmt.Errorf("cgroup DeviceAllow for %s: %w", devnode, err)
	}

	// Create the device node inside the container.
	dir := filepath.Dir(devnode)
	_, _ = r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
		"mkdir", "-p", dir)
	_, _ = r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
		"rm", "-f", devnode)
	if _, err := r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
		"mknod", devnode, "c", major, minor); err != nil {
		return fmt.Errorf("mknod %s: %w", devnode, err)
	}
	// Use restrictive permissions for video/media devices (0660 root:video)
	// matching the typical host access model. Other devices use 0666.
	if isVideoDevice(devnode) {
		if _, err := r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
			"chgrp", "video", devnode); err != nil {
			return fmt.Errorf("chgrp %s: %w", devnode, err)
		}
		if _, err := r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
			"chmod", "0660", devnode); err != nil {
			return fmt.Errorf("chmod %s: %w", devnode, err)
		}
	} else {
		if _, err := r.Run("sudo", "nsenter", "-t", pid, "-m", "--",
			"chmod", "0666", devnode); err != nil {
			return fmt.Errorf("chmod %s: %w", devnode, err)
		}
	}

	// Record in state directory.
	_, _ = r.Run("sudo", "mkdir", "-p", StateDir)
	stateFile := filepath.Join(StateDir, strings.ReplaceAll(devnode, "/", "_"))
	_ = sudo.WriteFile(r, stateFile, []byte(devnode+"\n"), 0644)

	return nil
}

// isVideoDevice reports whether the device path is a video or media controller device.
func isVideoDevice(devnode string) bool {
	base := filepath.Base(devnode)
	return strings.HasPrefix(base, "video") || strings.HasPrefix(base, "media")
}

// VideoDevice represents a detected video capture or media controller device.
type VideoDevice struct {
	DevNode string // e.g., /dev/video0, /dev/media0
	Name    string // human-readable name from sysfs
}

// DetectVideoDevices scans for V4L2 video and media controller devices.
func DetectVideoDevices() []VideoDevice {
	var devices []VideoDevice

	videoMatches, _ := filepath.Glob("/dev/video*")
	for _, dev := range videoMatches {
		name := readVideoSysfsName(dev)
		devices = append(devices, VideoDevice{DevNode: dev, Name: name})
	}

	mediaMatches, _ := filepath.Glob("/dev/media*")
	for _, dev := range mediaMatches {
		devices = append(devices, VideoDevice{DevNode: dev})
	}

	return devices
}

func readVideoSysfsName(devPath string) string {
	base := filepath.Base(devPath)
	data, err := os.ReadFile(filepath.Join("/sys/class/video4linux", base, "name"))
	if err != nil {
		return base
	}
	return strings.TrimSpace(string(data))
}

// RulesContent returns the YubiKey udev rules content (for testing).
func RulesContent() string {
	return rulesTemplate
}

// VideoRulesContent returns the video udev rules content (for testing).
func VideoRulesContent() string {
	return videoRulesTemplate
}

// ScriptContent returns the helper script content for the given machine name (for testing).
func ScriptContent(machine string) string {
	return scriptContent(machine)
}
