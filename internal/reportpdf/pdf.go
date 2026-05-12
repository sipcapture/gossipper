// Package reportpdf renders PDF reports using a headless Chromium/Chrome when available.
package reportpdf

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/stats"
)

// FromHTMLFile prints htmlPath to pdfPath using Chrome/Chromium --print-to-pdf.
func FromHTMLFile(htmlPath, pdfPath string) error {
	htmlAbs, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("report-pdf: html path: %w", err)
	}
	pdfAbs, err := filepath.Abs(pdfPath)
	if err != nil {
		return fmt.Errorf("report-pdf: pdf path: %w", err)
	}
	chrome := findChromium()
	if chrome == "" {
		return fmt.Errorf("report-pdf: install chromium or google-chrome and ensure it is in PATH")
	}
	url := "file://" + filepath.ToSlash(htmlAbs)
	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--print-to-pdf="+pdfAbs, url)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("report-pdf: chromium: %w", err)
	}
	return nil
}

// FromSummaryJSON renders summary JSON to HTML in a temp file, then prints to PDF.
func FromSummaryJSON(jsonPath, pdfPath string) error {
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("report-pdf: read json: %w", err)
	}
	var s stats.Summary
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("report-pdf: parse json: %w", err)
	}
	tmp, err := os.CreateTemp("", "gossipper-report-*.html")
	if err != nil {
		return fmt.Errorf("report-pdf: temp html: %w", err)
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpName) }()

	if err := reporthtml.WriteFile(tmpName, s); err != nil {
		return fmt.Errorf("report-pdf: html render: %w", err)
	}
	return FromHTMLFile(tmpName, pdfPath)
}

func findChromium() string {
	candidates := []string{
		"chromium",
		"chromium-browser",
		"google-chrome-stable",
		"google-chrome",
		"chrome",
	}
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p
		}
	}
	return ""
}

// InputKind returns "html", "json", or "" for unknown extension.
func InputKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "html"
	case ".json":
		return "json"
	default:
		return ""
	}
}
