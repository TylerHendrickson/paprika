package main

import (
	"runtime/debug"
	"strings"
)

var readBuildInfo = debug.ReadBuildInfo

// Intended to be overridden by -ldflags, e.g.:
//
//	go build -ldflags "-X main.BuildVersion=v1.2.3 -X main.BuildCommit=abc1234 -X main.BuildDate=2025-09-22T15:05:00Z -X main.BuildDirty=false"
var (
	BuildVersion = "dev"
	BuildCommit  = ""
	BuildDate    = ""
	BuildDirty   = ""
)

// enrichBuildInfo sets build variables from debug.BuildInfo if not provided via ldflags.
func enrichBuildInfo() {
	if BuildVersion != "dev" && BuildCommit != "" && BuildDate != "" {
		return
	}
	if bi, ok := readBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if BuildCommit == "" {
					BuildCommit = s.Value
				}
			case "vcs.time":
				if BuildDate == "" {
					BuildDate = s.Value
				}
			case "vcs.modified":
				if BuildDirty == "" {
					BuildDirty = s.Value
				}
			}
		}
	}
}

func versionStringShort() string {
	return BuildVersion
}

func versionStringFull() string {
	enrichBuildInfo()
	out := BuildVersion
	parts := []string{}
	if BuildCommit != "" {
		parts = append(parts, BuildCommit)
	}
	if BuildDate != "" {
		parts = append(parts, BuildDate)
	}
	if len(parts) > 0 {
		out += " (" + strings.Join(parts, ", ") + ")"
	}
	if BuildDirty == "true" {
		out += " [dirty]"
	}
	return out
}
