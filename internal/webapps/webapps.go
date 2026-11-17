// Package webapps finds and validates the Edge web apps (PWAs) that Microsoft
// 365 installs inside the Intune container.
//
// When a user installs a site as an app in Microsoft Edge ("Apps > Install
// this site as an app"), Edge writes 1 desktop file per app into the container
// user's ~/.local/share/applications/ directory, named
// msedge-<app id>-<profile>.desktop, for example
// msedge-ahjdipcglkflohmjbcjmjpidpbbiocpk-Default.desktop.
//
// The container user's home is a bind mount of the host's <home>/Intune
// directory (see cmd/start.go), so the files are read on the host from
// <home>/Intune/.local/share/applications/. Nothing here runs a command: the
// caller launches an app with the container's microsoft-edge wrapper, which
// applies the container flags, and uses SplitID to turn the ID into the
// --profile-directory and --app-id arguments. The desktop file only proves
// that the app is installed in that profile; its Exec line is never run, and
// never with gtk-launch, because that path skips the wrapper.
//
// Every ID comes from the user or from a desktop file name, so it is treated
// as untrusted: ValidID accepts only the strict Chromium app-ID shape, and the
// file checks stay inside the Intune directory with os.Root, so a symlink
// cannot point the read outside it and a FIFO cannot block it.
package webapps

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"unicode"
)

// AppsDir is the applications directory, relative to the Intune home
// directory (the host directory that is bound to the container user's home).
const AppsDir = ".local/share/applications"

// desktopEntryGroup is the group header that holds a desktop file's own keys.
const desktopEntryGroup = "[Desktop Entry]"

// maxDesktopSize bounds the read of 1 desktop file. A desktop file is a few
// KiB; a larger file is skipped rather than read.
const maxDesktopSize = 64 << 10

// maxNameRunes caps a display name, so a malformed name cannot grow without
// limit in a menu or a terminal table.
const maxNameRunes = 200

// idPattern is the only accepted ID shape: Chromium app IDs are 32 characters
// of a to p, and the profile part is "Default" or "Profile_<1..9999>"
// (Chromium makes it from the profile directory name and replaces a space with
// an underscore, so "Profile 1" becomes "Profile_1").
const idPattern = `^msedge-[a-p]{32}-(Default|Profile_[1-9][0-9]{0,3})$`

// fileSuffix is the file name suffix of every desktop file.
const fileSuffix = ".desktop"

var idRE = regexp.MustCompile(idPattern)

// ErrInvalidID reports an ID that does not match idPattern.
var ErrInvalidID = errors.New("invalid Edge web app ID")

// App is 1 Edge web app: the ID that names its desktop file, the name that
// Edge wrote into that file, and the Edge profile the app was installed in.
type App struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Profile string `json:"profile"`
}

// ValidID reports whether id is an Edge web app ID of the accepted shape.
func ValidID(id string) bool {
	return idRE.MatchString(id)
}

// SplitID splits a valid ID into the Chromium app ID (the 32 letters) and the
// Edge profile directory. "Default" stays "Default"; "Profile_<n>" becomes
// "Profile <n>", because Chromium replaced the space with an underscore in the
// file name. An ID that does not match idPattern gives an error that wraps
// ErrInvalidID.
func SplitID(id string) (appID, profileDir string, err error) {
	if !ValidID(id) {
		return "", "", fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	parts := strings.Split(id, "-")
	profileDir = parts[2]
	if n, ok := strings.CutPrefix(profileDir, "Profile_"); ok {
		profileDir = "Profile " + n
	}
	return parts[1], profileDir, nil
}

// Check reports whether id names an installed Edge web app: the ID must match
// idPattern, and <intuneHome>/.local/share/applications/<id>.desktop must be a
// regular file. A symlink (to a file inside or outside the Intune directory), a
// directory, a FIFO and a missing file are all refused. It reads no file
// contents, writes nothing and runs nothing.
func Check(intuneHome, id string) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	root, err := os.OpenRoot(intuneHome)
	if err != nil {
		return fmt.Errorf("open Intune directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	dir, err := root.OpenRoot(AppsDir)
	if err != nil {
		return fmt.Errorf("open Edge web app directory: %w", err)
	}
	defer func() { _ = dir.Close() }()

	name := id + fileSuffix
	info, err := dir.Lstat(name)
	if err != nil {
		return fmt.Errorf("edge web app %q: %w", id, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("edge web app %q is not a regular file", id)
	}
	return nil
}

// List returns the installed Edge web apps, sorted by name and then by ID. It
// returns an empty non-nil slice and no error when the applications directory
// (or the Intune directory) does not exist. A path that leads out of the Intune
// directory is an error. A file that fails any check is skipped, never fatal:
// the ID pattern, the regular-file test before and after the open, the 64 KiB
// size limit, and the read itself.
func List(intuneHome string) ([]App, error) {
	apps := []App{}

	root, err := os.OpenRoot(intuneHome)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return apps, nil
		}
		return nil, fmt.Errorf("open Intune directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	dir, err := root.OpenRoot(AppsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return apps, nil
		}
		return nil, fmt.Errorf("open Edge web app directory: %w", err)
	}
	defer func() { _ = dir.Close() }()

	dirFile, err := dir.Open(".")
	if err != nil {
		return nil, fmt.Errorf("read Edge web app directory: %w", err)
	}
	entries, err := dirFile.ReadDir(-1)
	closeErr := dirFile.Close()
	if err != nil {
		return nil, fmt.Errorf("read Edge web app directory: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("read Edge web app directory: %w", closeErr)
	}

	for _, entry := range entries {
		app, ok := readApp(dir, entry)
		if ok {
			apps = append(apps, app)
		}
	}

	slices.SortFunc(apps, func(a, b App) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return apps, nil
}

// readApp turns 1 directory entry into an App, and reports false when the entry
// is not a usable Edge web app desktop file.
func readApp(dir *os.Root, entry fs.DirEntry) (App, bool) {
	name := entry.Name()
	id, ok := strings.CutSuffix(name, fileSuffix)
	if !ok || !ValidID(id) {
		return App{}, false
	}

	info, err := dir.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return App{}, false
	}

	// O_NOFOLLOW, O_NONBLOCK and O_CLOEXEC keep a swapped-in symlink or FIFO
	// from redirecting or blocking the read.
	f, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return App{}, false
	}
	defer func() { _ = f.Close() }()

	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return App{}, false
	}

	data, err := io.ReadAll(io.LimitReader(f, maxDesktopSize+1))
	if err != nil || len(data) > maxDesktopSize {
		return App{}, false
	}

	display := parseName(data)
	if display == "" {
		display = id
	}
	return App{ID: id, Name: display, Profile: profileOf(id)}, true
}

// parseName returns the display name from the [Desktop Entry] group of a
// desktop file: the first plain Name= key in that group. A localized key such
// as Name[de]=, a key in another group, and a key before any group header are
// all ignored. Control characters are removed and the result is capped at
// maxNameRunes. It returns "" when the file holds no such key.
func parseName(data []byte) string {
	inDesktopEntry := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "[") {
			inDesktopEntry = strings.TrimSpace(line) == desktopEntryGroup
			continue
		}
		if !inDesktopEntry {
			continue
		}
		if value, ok := strings.CutPrefix(line, "Name="); ok {
			return cleanName(value)
		}
	}
	return ""
}

// cleanName trims surrounding spaces, drops control characters and caps the
// result at maxNameRunes runes.
func cleanName(name string) string {
	var b strings.Builder
	runes := 0
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsControl(r) {
			continue
		}
		if runes >= maxNameRunes {
			break
		}
		b.WriteRune(r)
		runes++
	}
	return b.String()
}

// profileOf returns the Edge profile part of a valid ID: "Default" or
// "Profile_<n>".
func profileOf(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) != 3 {
		return ""
	}
	return parts[2]
}
