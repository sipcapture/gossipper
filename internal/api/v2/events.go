package v2

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// handleJobEvents streams the worker's stats.jsonl as a long-lived response.
//
// Two modes:
//   - default: chunked text/plain; response holds open until the file stops
//     growing for `idle_ms` (default 5s) OR the request context cancels.
//   - ?since=<bytes>: returns only data starting at that byte offset; useful
//     for resuming a streamed read after a network blip. Pure replay (no
//     tail-following) when ?follow=false.
//   - ?tail=N: skip everything except the last N lines before starting.
//
// Response is plain JSONL (one stats snapshot per line) — same payload the
// worker writes. This is a poor-man's SSE; the UI can read with fetch() +
// ReadableStream, no special encoding.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
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
	path := filepath.Join(job.ArtifactsDir, "stats.jsonl")

	q := r.URL.Query()
	var since int64
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			since = n
		}
	}
	follow := true
	if v := strings.ToLower(strings.TrimSpace(q.Get("follow"))); v == "false" || v == "0" {
		follow = false
	}
	tail := 0
	if v := strings.TrimSpace(q.Get("tail")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	idleMs := 5000
	if v := strings.TrimSpace(q.Get("idle_ms")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			idleMs = n
		}
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)

	f, err := os.Open(path) //nolint:gosec // path derived from job.ArtifactsDir
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(w, `{"event":"absent","note":"stats.jsonl not produced yet"}`)
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()

	if tail > 0 {
		offset, err := tailOffset(f, tail)
		if err == nil {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				s.log.Warn("events: seek tail", "err", err)
			}
		}
	} else if since > 0 {
		if _, err := f.Seek(since, io.SeekStart); err != nil {
			s.log.Warn("events: seek since", "err", err)
		}
	}

	reader := bufio.NewReader(f)
	idle := time.Duration(idleMs) * time.Millisecond
	deadline := time.Now().Add(idle)

	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			deadline = time.Now().Add(idle)
			if _, werr := w.Write([]byte(line)); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return
			}
			if !follow {
				return
			}
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
	}
}

// tailOffset returns a byte offset such that reading from it yields the last
// `lines` lines (or fewer if the file is shorter). Best-effort; on read
// errors it returns 0 with the error so the caller can fall back to start.
func tailOffset(f *os.File, lines int) (int64, error) {
	stat, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := stat.Size()
	if size == 0 || lines <= 0 {
		return 0, nil
	}
	const chunk = 8 * 1024
	var (
		offset    = size
		buf       = make([]byte, chunk)
		seenLines int
	)
	for offset > 0 {
		read := int64(chunk)
		if offset < read {
			read = offset
		}
		offset -= read
		if _, err := f.ReadAt(buf[:read], offset); err != nil {
			return 0, err
		}
		for i := int(read) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				seenLines++
				if seenLines > lines {
					return offset + int64(i) + 1, nil
				}
			}
		}
	}
	return 0, nil
}
