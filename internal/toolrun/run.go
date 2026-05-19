package toolrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/launcher"
	"github.com/sipcapture/gossipper/internal/pcap2scenario"
	"github.com/sipcapture/gossipper/internal/pdf"
	"github.com/sipcapture/gossipper/internal/reporthtml"
	"github.com/sipcapture/gossipper/internal/stats"
	"github.com/sipcapture/gossipper/internal/supervisor"
	templ "github.com/sipcapture/gossipper/internal/template"
)

// Run executes a tool job described by spec (ProfileKind=tool).
func Run(ctx context.Context, spec supervisor.Spec) error {
	if !spec.IsToolJob() {
		return errors.New("toolrun: not a tool job")
	}
	tool := spec.ToolID()
	if tool == "" {
		return errors.New("toolrun: tool id is required")
	}
	if spec.DataDir == "" {
		return errors.New("toolrun: spec.data_dir is required")
	}
	args := spec.ToolArgs
	if args == nil {
		args = map[string]any{}
	}
	switch tool {
	case supervisor.ToolPCAP2Scenario:
		return runPCAP2Scenario(spec, args)
	case supervisor.ToolReportHTML:
		return runReportHTML(spec, args)
	case supervisor.ToolSummaryToPDF:
		return runSummaryToPDF(spec, args)
	case supervisor.ToolRTPSend:
		return runRTPSend(ctx, spec, args)
	case supervisor.ToolInfindex:
		return runInfindex(spec, args)
	default:
		return fmt.Errorf("toolrun: unknown tool %q", tool)
	}
}

// ResolveDataPath maps a relative path under dataDir to an absolute path.
func ResolveDataPath(dataDir, rel string) (string, error) {
	return resolveDataPath(dataDir, rel)
}

func resolveDataPath(dataDir, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("path is required")
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	var abs string
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	} else {
		abs = filepath.Clean(filepath.Join(root, rel))
	}
	if !pathWithinRoot(root, abs) {
		return "", fmt.Errorf("path %q escapes data-dir", rel)
	}
	return abs, nil
}

func pathWithinRoot(root, abs string) bool {
	if abs == root {
		return true
	}
	return strings.HasPrefix(abs, root+string(os.PathSeparator))
}

func argString(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func argInt(args map[string]any, key string, def int) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return def, nil
	}
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case int:
		return x, nil
	case json.Number:
		n, err := x.Int64()
		return int(n), err
	case string:
		if strings.TrimSpace(x) == "" {
			return def, nil
		}
		return strconv.Atoi(strings.TrimSpace(x))
	default:
		return 0, fmt.Errorf("%q must be an integer", key)
	}
}

func runPCAP2Scenario(spec supervisor.Spec, args map[string]any) error {
	pcapPath, err := resolveDataPath(spec.DataDir, argString(args, "pcap"))
	if err != nil {
		return fmt.Errorf("pcap: %w", err)
	}
	outDir := argString(args, "out_dir")
	if outDir == "" {
		outDir = filepath.Join(spec.ArtifactsDir, "scenarios")
	} else if outDir, err = resolveDataPath(spec.DataDir, outDir); err != nil {
		return fmt.Errorf("out_dir: %w", err)
	}
	sipPort, err := argInt(args, "sip_port", 0)
	if err != nil {
		return err
	}
	return pcap2scenario.Run(pcapPath, outDir, sipPort, argString(args, "pcap_link"))
}

func runReportHTML(spec supervisor.Spec, args map[string]any) error {
	inPath, err := resolveDataPath(spec.DataDir, argString(args, "in"))
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	outPath := filepath.Join(spec.ArtifactsDir, "report.html")
	if outRel := argString(args, "out"); outRel != "" {
		outPath, err = resolveDataPath(spec.DataDir, outRel)
		if err != nil {
			return fmt.Errorf("out: %w", err)
		}
	}
	raw, err := os.ReadFile(inPath)
	if err != nil {
		return err
	}
	var summary stats.Summary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return fmt.Errorf("parse summary json: %w", err)
	}
	return reporthtml.WriteFile(outPath, summary)
}

func runSummaryToPDF(spec supervisor.Spec, args map[string]any) error {
	inPath, err := resolveDataPath(spec.DataDir, argString(args, "in"))
	if err != nil {
		return fmt.Errorf("in: %w", err)
	}
	outPath := filepath.Join(spec.ArtifactsDir, "report.pdf")
	if outRel := argString(args, "out"); outRel != "" {
		outPath, err = resolveDataPath(spec.DataDir, outRel)
		if err != nil {
			return fmt.Errorf("out: %w", err)
		}
	}
	if err := pdf.TryRenderHTMLFileToPDF(inPath, outPath); err == nil {
		return nil
	} else if !errors.Is(err, pdf.ErrBuiltWithoutPDFTag) {
		return err
	}
	return renderPDFViaChromium(inPath, outPath)
}

func renderPDFViaChromium(htmlPath, pdfPath string) error {
	candidates := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	var chrome string
	for _, name := range candidates {
		if p, err := exec.LookPath(name); err == nil {
			chrome = p
			break
		}
	}
	if chrome == "" {
		return errors.New("summary-to-pdf: no chromium/google-chrome in PATH and binary built without -tags pdf")
	}
	tmpDir, err := os.MkdirTemp("", "gossipper-pdf-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	fileURL := "file://" + filepath.ToSlash(htmlPath)
	cmd := exec.Command(chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-sandbox",
		"--user-data-dir="+tmpDir,
		"--print-to-pdf="+pdfPath,
		fileURL,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("summary-to-pdf: %w", err)
	}
	return nil
}

func runRTPSend(ctx context.Context, spec supervisor.Spec, args map[string]any) error {
	addr := argString(args, "rtp_addr")
	if addr == "" {
		return errors.New("rtp_addr is required")
	}
	freq, err := argInt(args, "rtp_freq_ms", 20)
	if err != nil {
		return err
	}
	durMs, err := argInt(args, "duration_ms", 0)
	if err != nil {
		return err
	}
	cfg := cli.DefaultConfig()
	cfg.RTPSend = true
	cfg.RTPAddr = addr
	cfg.RTPCodec = argString(args, "rtp_codec")
	if cfg.RTPCodec == "" {
		cfg.RTPCodec = "PCMU/8000"
	}
	cfg.RTPFreqMs = freq
	cfg.TotalCalls = 1
	cfg.TotalCallsSetExplicitly = true
	if durMs > 0 {
		cfg.GlobalTimeout = time.Duration(durMs) * time.Millisecond
		runCtx, cancel := context.WithTimeout(ctx, cfg.GlobalTimeout)
		defer cancel()
		return launcher.RunRTPSender(runCtx, cfg)
	}
	return launcher.RunRTPSender(ctx, cfg)
}

func runInfindex(spec supervisor.Spec, args map[string]any) error {
	csvPath, err := resolveDataPath(spec.DataDir, argString(args, "csv"))
	if err != nil {
		return fmt.Errorf("csv: %w", err)
	}
	field, err := argInt(args, "field", 0)
	if err != nil {
		return err
	}
	if field < 0 {
		return errors.New("field must be >= 0")
	}
	_, _, err = templ.GenerateCSVIndex("", csvPath, field)
	return err
}
