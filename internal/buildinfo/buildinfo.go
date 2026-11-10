// Package buildinfo identifies the running kubebao build.
package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version and Commit are set at link time by the build and distribution targets.
var (
	Version = "dev"
	Commit  = ""
)

// Info describes a compiled binary. It contains no Kubernetes configuration.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Current returns link-time metadata with embedded Go build information as a fallback.
func Current() Info {
	embedded, _ := debug.ReadBuildInfo()
	version, commit := fromBuildInfo(Version, Commit, embedded)
	return Info{
		Version: version, Commit: commit, GoVersion: runtime.Version(),
		OS: runtime.GOOS, Arch: runtime.GOARCH,
	}
}

func (i Info) String() string {
	return fmt.Sprintf("kubebao %s (commit %s)", i.Version, i.Commit)
}

func fromBuildInfo(version, commit string, info *debug.BuildInfo) (string, string) {
	if info != nil {
		if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		if commit == "" {
			dirty := false
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					commit = setting.Value
				case "vcs.modified":
					dirty = setting.Value == "true"
				}
			}
			if dirty && commit != "" {
				commit += "-dirty"
			}
		}
	}
	if commit == "" {
		commit = "unknown"
	}
	return version, commit
}
