// Package layout is where install.sh puts the smithy binary: the versions in
// $XDG_DATA_HOME/smithy/versions/<version> (default ~/.local/share), the command as a link at
// $XDG_BIN_HOME/smithy (default ~/.local/bin); on Windows %LOCALAPPDATA%\smithy\versions and
// %LOCALAPPDATA%\Programs\smithy. Data and config stay in the smithy home (~/.smithy).
package layout

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/Zovaris/smithy/internal/ghx"
)

// UserHome is $HOME, else the OS user home.
func UserHome() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func localAppData() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return d
	}
	return filepath.Join(UserHome(), "AppData", "Local")
}

// DataDir holds the installed versions and dev builds.
func DataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData(), "smithy")
	}
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "smithy")
	}
	return filepath.Join(UserHome(), ".local", "share", "smithy")
}

// VersionsDir holds one folder per installed version.
func VersionsDir() string { return filepath.Join(DataDir(), "versions") }

// DevDir holds the launcher's per-checkout dev builds.
func DevDir() string { return filepath.Join(DataDir(), "dev") }

// BinDir is the folder on PATH that holds the smithy command.
func BinDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(localAppData(), "Programs", "smithy")
	}
	if d := os.Getenv("XDG_BIN_HOME"); filepath.IsAbs(d) {
		return d
	}
	return filepath.Join(UserHome(), ".local", "bin")
}

// Exe is the binary's file name on this OS.
func Exe() string {
	if runtime.GOOS == "windows" {
		return "smithy.exe"
	}
	return "smithy"
}

// Link is the smithy command on PATH.
func Link() string { return filepath.Join(BinDir(), Exe()) }

// LegacyBin is the pre-0.21 install folder (~/.smithy/bin), kept as links for one release.
func LegacyBin() string { return filepath.Join(ghx.Home(), "bin") }

// SkillsManifest lists, one per line, the skill folders `smithy skills install` wrote.
func SkillsManifest() string { return filepath.Join(ghx.Home(), "installed-skills") }

// PathRecord lists, one per line, the shell startup files a PATH line was added to.
func PathRecord() string { return filepath.Join(ghx.Home(), "path-added") }

// PathMarker precedes the PATH line install.sh and `smithy setup path add` write.
const PathMarker = "# smithy: run `smithy` from any terminal"

// OnPath reports whether dir is an entry of $PATH.
func OnPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != "" && filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
