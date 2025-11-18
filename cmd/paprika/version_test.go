package main

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

type buildVarsSet struct {
	version, commit, date, dirty string
	readBuildInfo                func() (*debug.BuildInfo, bool)
}

func (b buildVarsSet) apply() {
	BuildVersion = b.version
	BuildCommit = b.commit
	BuildDate = b.date
	BuildDirty = b.dirty

	if b.readBuildInfo != nil {
		readBuildInfo = b.readBuildInfo
	}
}

// resetBuildVarsTestCleanup resets the globals and restores readBuildInfo after each test.
func resetBuildVarsTestCleanup(t *testing.T) {
	orig := buildVarsSet{
		version:       BuildVersion,
		commit:        BuildCommit,
		date:          BuildDate,
		dirty:         BuildDirty,
		readBuildInfo: readBuildInfo,
	}
	t.Cleanup(orig.apply)
}

func TestVersionStringShort(t *testing.T) {
	resetBuildVarsTestCleanup(t)

	testBuildVersion := "v1.2.3"
	BuildVersion = testBuildVersion
	assert.Equal(t, testBuildVersion, versionStringShort())
}

func TestVersionStringFull(t *testing.T) {
	for _, tt := range []struct {
		name      string
		buildVars buildVarsSet
		want      string
	}{
		{
			name:      "no enrichment",
			buildVars: buildVarsSet{version: "v1.0.0", commit: "abcdef1", date: "2025-09-22T15:05:00Z", dirty: "false"},
			want:      "v1.0.0 (abcdef1, 2025-09-22T15:05:00Z)",
		},
		{
			name:      "only commit",
			buildVars: buildVarsSet{version: "v1.0.0", commit: "abc1234"},
			want:      "v1.0.0 (abc1234)",
		},
		{
			name:      "only date",
			buildVars: buildVarsSet{version: "v1.0.0", date: "2025-01-02T03:04:05Z"},
			want:      "v1.0.0 (2025-01-02T03:04:05Z)",
		},
		{
			name:      "dirty suffix",
			buildVars: buildVarsSet{version: "v1.0.0", commit: "abc", date: "2025-01-01T00:00:00Z", dirty: "true"},
			want:      "v1.0.0 (abc, 2025-01-01T00:00:00Z) [dirty]",
		},
		{
			name: "defers to ReadBuildInfo",
			buildVars: buildVarsSet{version: "dev", readBuildInfo: func() (*debug.BuildInfo, bool) {
				return &debug.BuildInfo{
					Settings: []debug.BuildSetting{
						{Key: "vcs.revision", Value: "revX"},
						{Key: "vcs.time", Value: "timeX"},
						{Key: "vcs.modified", Value: "true"},
					},
				}, true
			}},
			want: "dev (revX, timeX) [dirty]",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetBuildVarsTestCleanup(t)

			tt.buildVars.apply()
			got := versionStringFull()
			assert.Equal(t, tt.want, got)
		})
	}
}
