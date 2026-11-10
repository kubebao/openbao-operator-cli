package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestBuildMetadataPrecedence(t *testing.T) {
	t.Parallel()
	embedded := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.1.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
	cases := []struct {
		name, version, commit, wantVersion, wantCommit string
		info                                           *debug.BuildInfo
	}{
		{"link time", "v0.2.0", "release-commit", "v0.2.0", "release-commit", embedded},
		{"embedded", "dev", "", "v0.1.0", "0123456789abcdef-dirty", embedded},
		{"no metadata", "dev", "", "dev", "unknown", nil},
		{"development module", "dev", "", "dev", "unknown", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version, commit := fromBuildInfo(tc.version, tc.commit, tc.info)
			if version != tc.wantVersion || commit != tc.wantCommit {
				t.Fatalf("build metadata = %q %q, want %q %q", version, commit, tc.wantVersion, tc.wantCommit)
			}
		})
	}
}
