package main

import (
	"fmt"
	"runtime"
	"time"
)

var (
	Version   = "0.1.52"
	BuildDate = "1970-01-01"
	BuildTime = "00:00:00"
	GitCommit = "unknown"
	GoVersion = runtime.Version()
	BuildOS   = runtime.GOOS
	BuildArch = runtime.GOARCH
)

type VersionInfo struct {
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	BuildTime string `json:"build_time"`
	GitCommit string `json:"git_commit"`
	GoVersion string `json:"go_version"`
	BuildOS   string `json:"build_os"`
	BuildArch string `json:"build_arch"`
}

func GetVersionInfo() VersionInfo {
	return VersionInfo{
		Version:   Version,
		BuildDate: BuildDate,
		BuildTime: BuildTime,
		GitCommit: GitCommit,
		GoVersion: GoVersion,
		BuildOS:   BuildOS,
		BuildArch: BuildArch,
	}
}

func GetVersionString() string {
	return fmt.Sprintf("Gossipper %s\nbuilt %s %s, commit %s, go %s, %s/%s",
		Version, BuildDate, BuildTime, GitCommit, GoVersion, BuildOS, BuildArch)
}

func GetShortVersionString() string {
	return fmt.Sprintf("Gossipper %s", Version)
}

func PrintVersion() {
	fmt.Println(GetVersionString())
}

func GetBuildTimestamp() time.Time {
	timestamp := fmt.Sprintf("%s %s", BuildDate, BuildTime)
	if t, err := time.Parse("2006-01-02 15:04:05", timestamp); err == nil {
		return t
	}
	return time.Time{}
}
