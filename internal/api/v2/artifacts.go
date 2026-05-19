package v2

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipcapture/gossipper/internal/supervisor"
)

var artifactKindFallback = map[string]string{
	"summary":      "summary.json",
	"report_html":  "report.html",
	"report_pdf":   "report.pdf",
	"log":          "worker.log",
	"stats":        "stats.jsonl",
	"call_records": "call_records.jsonl",
}

func (s *Server) handleDownloadJobArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	job, err := s.cfg.Registry.Store.Get(r.Context(), pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if strings.TrimSpace(job.ArtifactsDir) == "" {
		s.writeError(w, http.StatusNotFound, "job has no artifacts dir")
		return
	}
	kind := strings.TrimSpace(r.PathValue("kind"))
	if kind == "" {
		s.writeError(w, http.StatusBadRequest, "artifact kind is required")
		return
	}
	path, err := s.resolveJobArtifactPath(r.Context(), job, kind)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeError(w, http.StatusNotFound, "artifact not found")
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) resolveJobArtifactPath(ctx context.Context, job supervisor.Job, kind string) (string, error) {
	arts, err := s.cfg.Registry.Store.ListArtifacts(ctx, job.ID)
	if err != nil {
		return "", err
	}
	for i := len(arts) - 1; i >= 0; i-- {
		if arts[i].Kind == kind {
			if pathWithinDir(job.ArtifactsDir, arts[i].Path) {
				if _, statErr := os.Stat(arts[i].Path); statErr == nil {
					return arts[i].Path, nil
				}
			}
		}
	}
	if base, ok := artifactKindFallback[kind]; ok {
		candidate := filepath.Join(job.ArtifactsDir, base)
		if pathWithinDir(job.ArtifactsDir, candidate) {
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
			return candidate, os.ErrNotExist
		}
	}
	return "", os.ErrNotExist
}

func pathWithinDir(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if targetAbs == rootAbs {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(targetAbs, rootAbs+sep)
}
