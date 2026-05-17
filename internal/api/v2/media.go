package v2

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sipcapture/gossipper/internal/uistore"
)

const maxMediaUploadBytes = 256 << 20 // 256 MiB

func parseMediaKind(s string) (uistore.MediaKind, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "wav":
		return uistore.MediaWav, true
	case "pcap":
		return uistore.MediaPcap, true
	default:
		return "", false
	}
}

func (s *Server) handleListMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	kind, ok := parseMediaKind(r.PathValue("kind"))
	if !ok {
		s.writeError(w, http.StatusBadRequest, "kind must be 'wav' or 'pcap'")
		return
	}
	out, err := s.cfg.Store.ListMedia(kind)
	if err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	if out == nil {
		out = []uistore.MediaAsset{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"media": out, "kind": string(kind)})
}

func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	kind, ok := parseMediaKind(r.PathValue("kind"))
	if !ok {
		s.writeError(w, http.StatusBadRequest, "kind must be 'wav' or 'pcap'")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		s.writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	defer r.Body.Close()
	limited := http.MaxBytesReader(w, r.Body, maxMediaUploadBytes)
	asset, err := s.cfg.Store.PutMedia(kind, name, limited)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, fmt.Sprintf("upload: %v", err))
		return
	}
	s.writeAudit(r.Context(), r, "media.upload", string(kind)+"/"+asset.Name, "")
	s.writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) handleDownloadMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	kind, ok := parseMediaKind(r.PathValue("kind"))
	if !ok {
		s.writeError(w, http.StatusBadRequest, "kind must be 'wav' or 'pcap'")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	path, err := s.cfg.Store.MediaPath(kind, name)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	if !s.requireStore(w) {
		return
	}
	kind, ok := parseMediaKind(r.PathValue("kind"))
	if !ok {
		s.writeError(w, http.StatusBadRequest, "kind must be 'wav' or 'pcap'")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if err := s.cfg.Store.DeleteMedia(kind, name); err != nil {
		code, msg := mapStoreError(err)
		s.writeError(w, code, msg)
		return
	}
	s.writeAudit(r.Context(), r, "media.delete", string(kind)+"/"+name, "")
	w.WriteHeader(http.StatusNoContent)
}
