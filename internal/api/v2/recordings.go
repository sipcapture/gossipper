package v2

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type recordingEntry struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

func (s *Server) handleListRecordings(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	job, err := s.cfg.Registry.Store.Get(r.Context(), pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	dir := filepath.Join(job.ArtifactsDir, "recordings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			s.writeJSON(w, http.StatusOK, map[string]any{"recordings": []recordingEntry{}})
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]recordingEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, recordingEntry{
			Name:      e.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC().Format(http.TimeFormat),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	s.writeJSON(w, http.StatusOK, map[string]any{"recordings": out})
}

func (s *Server) handleDownloadRecording(w http.ResponseWriter, r *http.Request) {
	if !s.requireRegistry(w) {
		return
	}
	job, err := s.cfg.Registry.Store.Get(r.Context(), pathID(r))
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		s.writeError(w, http.StatusBadRequest, "invalid recording name")
		return
	}
	path := filepath.Join(job.ArtifactsDir, "recordings", name)
	if _, err := os.Stat(path); err != nil {
		s.writeError(w, http.StatusNotFound, "recording not found")
		return
	}
	http.ServeFile(w, r, path)
}
