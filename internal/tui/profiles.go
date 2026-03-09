package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type profile struct {
	Name         string
	Mode         string
	ScenarioName string
	ScenarioFile string
	Description  string
	Custom       bool
}

func loadProfiles() []profile {
	profiles := []profile{
		{
			Name:         "builtin-uac",
			Mode:         "client",
			ScenarioName: "uac",
			Description:  "Built-in basic UAC scenario",
		},
		{
			Name:         "builtin-uas",
			Mode:         "server",
			ScenarioName: "uas",
			Description:  "Built-in basic UAS scenario",
		},
	}

	entries, err := os.ReadDir("testdata/scenarios")
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".xml") {
				continue
			}
			name := entry.Name()
			profiles = append(profiles, profile{
				Name:         strings.TrimSuffix(name, ".xml"),
				Mode:         inferProfileMode(name),
				ScenarioFile: filepath.Join("testdata", "scenarios", name),
				Description:  "Repository XML fixture",
			})
		}
	}

	slices.SortFunc(profiles[2:], func(a, b profile) int {
		return strings.Compare(a.Name, b.Name)
	})

	profiles = append(profiles, profile{
		Name:        "custom-xml",
		Mode:        "client",
		Description: "Custom XML scenario file",
		Custom:      true,
	})
	return profiles
}

func filterProfiles(profiles []profile, mode string) []profile {
	filtered := make([]profile, 0, len(profiles))
	for _, item := range profiles {
		if item.Mode == mode || item.Custom {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return []profile{{Name: "custom-xml", Mode: mode, Description: "Custom XML scenario file", Custom: true}}
	}
	return filtered
}

func profileLabels(profiles []profile) []string {
	labels := make([]string, 0, len(profiles))
	for _, item := range profiles {
		label := item.Name
		if item.ScenarioName != "" {
			label = item.Name + " (" + item.ScenarioName + ")"
		}
		labels = append(labels, label)
	}
	return labels
}

func inferProfileMode(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "uas"), strings.Contains(lower, "server"):
		return "server"
	default:
		return "client"
	}
}
