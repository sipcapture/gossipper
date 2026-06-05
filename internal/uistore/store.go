package uistore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned when a profile / scenario / media asset is missing.
var ErrNotFound = errors.New("uistore: not found")

// ErrConflict is returned when a create operation hits an existing ID.
var ErrConflict = errors.New("uistore: id already exists")

// ErrInvalidID is returned when an id contains forbidden characters.
var ErrInvalidID = errors.New("uistore: invalid id (allowed: [a-zA-Z0-9._-], 1..64 chars)")

// ErrInvalidHistoryTS is returned when a snapshot-id (history "ts" segment)
// does not match the safe filename pattern (digits + T/Z/.).
var ErrInvalidHistoryTS = errors.New("uistore: invalid history ts (allowed: digits, T, ., Z; 16..40 chars)")

// StoreOptions tunes optional Store behaviour.
type StoreOptions struct {
	// ScenarioHistoryKeep caps archived scenario versions per id. 0 keeps all
	// snapshots (default). After each new snapshot the oldest entries beyond
	// the cap are deleted automatically.
	ScenarioHistoryKeep int
}

// Store wraps a Layout with concurrency-safe CRUD helpers.
type Store struct {
	mu                  sync.RWMutex
	layout              Layout
	now                 func() time.Time
	scenarioHistoryKeep int
}

// Open returns a Store rooted at the given directory; missing sub-dirs are
// created.
func Open(root string) (*Store, error) {
	return OpenWithOptions(root, StoreOptions{})
}

// OpenWithOptions is like Open but honours optional tuning knobs.
func OpenWithOptions(root string, opt StoreOptions) (*Store, error) {
	l, err := New(root)
	if err != nil {
		return nil, err
	}
	if err := l.Ensure(); err != nil {
		return nil, err
	}
	keep := opt.ScenarioHistoryKeep
	if keep < 0 {
		keep = 0
	}
	return &Store{
		layout:              l,
		now:                 func() time.Time { return time.Now().UTC() },
		scenarioHistoryKeep: keep,
	}, nil
}

// Layout exposes the underlying directory layout (read only).
func (s *Store) Layout() Layout { return s.layout }

// ScenarioHistoryKeep returns the configured cap on archived scenario versions
// per id (0 = unlimited).
func (s *Store) ScenarioHistoryKeep() int { return s.scenarioHistoryKeep }

func isSafeID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return false
		}
	}
	if id == "." || id == ".." {
		return false
	}
	return true
}

// --------------- atomic file helpers ---------------

func (s *Store) writeAtomic(targetPath string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.layout.TempDir(), "uistore-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		cleanup()
		return err
	}
	return nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("uistore: parse %s: %w", filepath.Base(path), err)
	}
	return nil
}

func marshalJSON(v any) ([]byte, error) {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	return out, nil
}

// --------------- server profiles ---------------

func (s *Store) serverPath(id string) string {
	return filepath.Join(s.layout.ServersDir(), id+".json")
}

// ListServerProfiles returns server profiles sorted by id.
func (s *Store) ListServerProfiles() ([]ServerProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return listJSON[ServerProfile](s.layout.ServersDir())
}

// GetServerProfile fetches a server profile by id.
func (s *Store) GetServerProfile(id string) (ServerProfile, error) {
	if !isSafeID(id) {
		return ServerProfile{}, ErrInvalidID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var p ServerProfile
	if err := readJSON(s.serverPath(id), &p); err != nil {
		return ServerProfile{}, err
	}
	return p, nil
}

// PutServerProfile creates or updates a server profile.
// When create=true the function fails with ErrConflict if the id exists.
func (s *Store) PutServerProfile(p ServerProfile, create bool) (ServerProfile, error) {
	if !isSafeID(p.ID) {
		return ServerProfile{}, ErrInvalidID
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = p.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	path := s.serverPath(p.ID)
	if _, err := os.Stat(path); err == nil {
		if create {
			return ServerProfile{}, ErrConflict
		}
		var existing ServerProfile
		if err := readJSON(path, &existing); err == nil {
			if p.CreatedAt.IsZero() {
				p.CreatedAt = existing.CreatedAt
			}
		}
	} else {
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
	}
	p.UpdatedAt = now
	data, err := marshalJSON(&p)
	if err != nil {
		return ServerProfile{}, err
	}
	if err := s.writeAtomic(path, data, 0o640); err != nil {
		return ServerProfile{}, err
	}
	return p, nil
}

// DeleteServerProfile removes a server profile by id.
func (s *Store) DeleteServerProfile(id string) error {
	if !isSafeID(id) {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.serverPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// --------------- client profiles ---------------

func (s *Store) clientPath(id string) string {
	return filepath.Join(s.layout.ClientsDir(), id+".json")
}

// ListClientProfiles returns client profiles sorted by id.
func (s *Store) ListClientProfiles() ([]ClientProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return listJSON[ClientProfile](s.layout.ClientsDir())
}

// GetClientProfile fetches a client profile by id.
func (s *Store) GetClientProfile(id string) (ClientProfile, error) {
	if !isSafeID(id) {
		return ClientProfile{}, ErrInvalidID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var p ClientProfile
	if err := readJSON(s.clientPath(id), &p); err != nil {
		return ClientProfile{}, err
	}
	return p, nil
}

// PutClientProfile creates or updates a client profile.
func (s *Store) PutClientProfile(p ClientProfile, create bool) (ClientProfile, error) {
	if !isSafeID(p.ID) {
		return ClientProfile{}, ErrInvalidID
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		p.Name = p.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	path := s.clientPath(p.ID)
	if _, err := os.Stat(path); err == nil {
		if create {
			return ClientProfile{}, ErrConflict
		}
		var existing ClientProfile
		if err := readJSON(path, &existing); err == nil {
			if p.CreatedAt.IsZero() {
				p.CreatedAt = existing.CreatedAt
			}
		}
	} else {
		if p.CreatedAt.IsZero() {
			p.CreatedAt = now
		}
	}
	p.UpdatedAt = now
	data, err := marshalJSON(&p)
	if err != nil {
		return ClientProfile{}, err
	}
	if err := s.writeAtomic(path, data, 0o640); err != nil {
		return ClientProfile{}, err
	}
	return p, nil
}

// DeleteClientProfile removes a client profile by id.
func (s *Store) DeleteClientProfile(id string) error {
	if !isSafeID(id) {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.clientPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// --------------- scenarios ---------------

func (s *Store) scenarioXMLPath(id string) string {
	return filepath.Join(s.layout.ScenariosDir(), id+".xml")
}

func (s *Store) scenarioMetaPath(id string) string {
	return filepath.Join(s.layout.ScenariosDir(), id+".meta.json")
}

// ListScenarios returns scenario metadata for every *.meta.json found.
// Scenarios without a sidecar are reported with synthesised meta.
func (s *Store) ListScenarios() ([]ScenarioMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dir := s.layout.ScenariosDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	type rec struct {
		id      string
		hasXML  bool
		hasMeta bool
		modTime time.Time
	}
	byID := map[string]*rec{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(name, ".meta.json"):
			id := strings.TrimSuffix(name, ".meta.json")
			r := byID[id]
			if r == nil {
				r = &rec{id: id}
				byID[id] = r
			}
			r.hasMeta = true
			if info.ModTime().After(r.modTime) {
				r.modTime = info.ModTime()
			}
		case strings.HasSuffix(name, ".xml"):
			id := strings.TrimSuffix(name, ".xml")
			r := byID[id]
			if r == nil {
				r = &rec{id: id}
				byID[id] = r
			}
			r.hasXML = true
			if info.ModTime().After(r.modTime) {
				r.modTime = info.ModTime()
			}
		}
	}
	out := make([]ScenarioMeta, 0, len(byID))
	for id, r := range byID {
		if !r.hasXML {
			continue
		}
		if r.hasMeta {
			var m ScenarioMeta
			if err := readJSON(s.scenarioMetaPath(id), &m); err == nil {
				if m.ID == "" {
					m.ID = id
				}
				out = append(out, m)
				continue
			}
		}
		out = append(out, ScenarioMeta{
			ID:        id,
			Name:      id,
			UpdatedAt: r.modTime.UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ScenarioBody represents the XML body + sidecar meta of a scenario.
type ScenarioBody struct {
	Meta ScenarioMeta `json:"meta"`
	XML  string       `json:"xml"`
}

// GetScenario returns XML body + meta for an id.
func (s *Store) GetScenario(id string) (ScenarioBody, error) {
	if !isSafeID(id) {
		return ScenarioBody{}, ErrInvalidID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	xmlPath := s.scenarioXMLPath(id)
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ScenarioBody{}, ErrNotFound
		}
		return ScenarioBody{}, err
	}
	var meta ScenarioMeta
	if err := readJSON(s.scenarioMetaPath(id), &meta); err != nil && !errors.Is(err, ErrNotFound) {
		return ScenarioBody{}, err
	}
	if meta.ID == "" {
		meta.ID = id
	}
	if meta.Name == "" {
		meta.Name = id
	}
	return ScenarioBody{Meta: meta, XML: string(data)}, nil
}

// PutScenario creates or updates an XML scenario and its sidecar meta.
// When create=true the function fails with ErrConflict if the id exists.
//
// On update (create=false and the scenario already exists), the prior XML+meta
// is snapshotted into scenarios/<id>.history/<ts>.{xml,meta.json} before the
// new content lands. Snapshots are skipped when the new XML is byte-identical
// to the on-disk copy (treats sidecar-only edits as non-versioned).
func (s *Store) PutScenario(meta ScenarioMeta, xml string, create bool) (ScenarioBody, error) {
	if !isSafeID(meta.ID) {
		return ScenarioBody{}, ErrInvalidID
	}
	if strings.TrimSpace(xml) == "" {
		return ScenarioBody{}, fmt.Errorf("uistore: scenario XML is empty")
	}
	meta.Name = strings.TrimSpace(meta.Name)
	if meta.Name == "" {
		meta.Name = meta.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	xmlPath := s.scenarioXMLPath(meta.ID)
	metaPath := s.scenarioMetaPath(meta.ID)
	if _, err := os.Stat(xmlPath); err == nil {
		if create {
			return ScenarioBody{}, ErrConflict
		}
		var existing ScenarioMeta
		if err := readJSON(metaPath, &existing); err == nil {
			if meta.CreatedAt.IsZero() {
				meta.CreatedAt = existing.CreatedAt
			}
		}
		// Snapshot prior version before overwriting. Best-effort: a failure
		// here logs through the returned error path, but only if the snapshot
		// itself was attempted with new content. Identical content → skip.
		priorXML, rerr := os.ReadFile(xmlPath)
		if rerr == nil && string(priorXML) != xml {
			if err := s.snapshotScenarioLocked(meta.ID, priorXML, existing, now); err != nil {
				return ScenarioBody{}, fmt.Errorf("uistore: snapshot prior scenario: %w", err)
			}
		}
	} else {
		if meta.CreatedAt.IsZero() {
			meta.CreatedAt = now
		}
	}
	meta.UpdatedAt = now
	if err := s.writeAtomic(xmlPath, []byte(xml), 0o640); err != nil {
		return ScenarioBody{}, err
	}
	data, err := marshalJSON(&meta)
	if err != nil {
		return ScenarioBody{}, err
	}
	if err := s.writeAtomic(metaPath, data, 0o640); err != nil {
		return ScenarioBody{}, err
	}
	return ScenarioBody{Meta: meta, XML: xml}, nil
}

// DeleteScenario removes both XML, sidecar, and the entire history dir.
func (s *Store) DeleteScenario(id string) error {
	if !isSafeID(id) {
		return ErrInvalidID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	xmlErr := os.Remove(s.scenarioXMLPath(id))
	metaErr := os.Remove(s.scenarioMetaPath(id))
	if xmlErr != nil && !errors.Is(xmlErr, os.ErrNotExist) {
		return xmlErr
	}
	if metaErr != nil && !errors.Is(metaErr, os.ErrNotExist) {
		return metaErr
	}
	// History directory is cosmetic — drop silently on any non-ENOENT error
	// rather than failing the whole delete; callers cannot observe leftover
	// history because there's no XML to anchor it to anymore.
	if histDir, err := s.layout.ScenarioHistoryDir(id); err == nil {
		_ = os.RemoveAll(histDir)
	}
	if errors.Is(xmlErr, os.ErrNotExist) && errors.Is(metaErr, os.ErrNotExist) {
		return ErrNotFound
	}
	return nil
}

// historyTSFormat is the filename-safe timestamp layout for snapshots. We
// avoid `:` (Windows) and rely on nanosecond precision to dodge same-tick
// collisions when two updates land back-to-back.
const historyTSFormat = "20060102T150405.000000000Z"

// isSafeHistoryTS validates a user-supplied snapshot identifier. It must look
// like one of our generated stamps so we never traverse outside the history
// dir.
func isSafeHistoryTS(ts string) bool {
	if len(ts) < 16 || len(ts) > 40 {
		return false
	}
	for _, r := range ts {
		switch {
		case r >= '0' && r <= '9':
		case r == 'T' || r == 'Z' || r == '.':
		default:
			return false
		}
	}
	return true
}

// snapshotScenarioLocked writes priorXML + priorMeta into the per-scenario
// history directory keyed by a nanosecond-precision UTC timestamp. The caller
// must already hold s.mu (write).
func (s *Store) snapshotScenarioLocked(id string, priorXML []byte, priorMeta ScenarioMeta, now time.Time) error {
	id = strings.TrimSpace(id)
	if !isSafeID(id) {
		return ErrInvalidID
	}
	dir, err := s.layout.ScenarioHistoryDir(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	ts := now.UTC().Format(historyTSFormat)
	xmlOut := filepath.Join(dir, ts+".xml")
	metaOut := filepath.Join(dir, ts+".meta.json")
	if err := s.writeAtomic(xmlOut, priorXML, 0o640); err != nil {
		return err
	}
	if priorMeta.ID == "" {
		priorMeta.ID = id
	}
	data, err := marshalJSON(&priorMeta)
	if err != nil {
		return err
	}
	if err := s.writeAtomic(metaOut, data, 0o640); err != nil {
		return err
	}
	return s.pruneScenarioHistoryLocked(id)
}

// pruneScenarioHistoryLocked drops the oldest snapshots when count exceeds
// ScenarioHistoryKeep. Caller must hold s.mu (write).
func (s *Store) pruneScenarioHistoryLocked(id string) error {
	if s.scenarioHistoryKeep <= 0 {
		return nil
	}
	id = strings.TrimSpace(id)
	if !isSafeID(id) {
		return ErrInvalidID
	}
	dir, err := s.layout.ScenarioHistoryDir(id)
	if err != nil {
		return err
	}
	entries, err := listScenarioHistoryEntriesFromDir(dir)
	if err != nil {
		return err
	}
	for i := s.scenarioHistoryKeep; i < len(entries); i++ {
		ts := entries[i].TS
		_ = os.Remove(filepath.Join(dir, ts+".xml"))
		_ = os.Remove(filepath.Join(dir, ts+".meta.json"))
	}
	return nil
}

// listScenarioHistoryEntriesFromDir scans dir for *.xml snapshots, newest first.
func listScenarioHistoryEntriesFromDir(dir string) ([]ScenarioHistoryEntry, error) {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "." || dir == "" || filepath.IsAbs(dir) {
		return nil, fmt.Errorf("uistore: invalid history directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ScenarioHistoryEntry{}, nil
		}
		return nil, err
	}
	out := make([]ScenarioHistoryEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".xml") {
			continue
		}
		ts := strings.TrimSuffix(name, ".xml")
		if !isSafeHistoryTS(ts) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		parsed, perr := time.Parse(historyTSFormat, ts)
		if perr != nil {
			parsed = info.ModTime().UTC()
		}
		entry := ScenarioHistoryEntry{
			TS:        ts,
			Timestamp: parsed.UTC(),
			SizeBytes: info.Size(),
		}
		var meta ScenarioMeta
		if err := readJSON(filepath.Join(dir, ts+".meta.json"), &meta); err == nil {
			entry.Meta = meta
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TS > out[j].TS })
	return out, nil
}

// ListScenarioHistory returns archived prior versions of a scenario, newest
// first. Missing history directory (no updates yet) returns an empty slice.
func (s *Store) ListScenarioHistory(id string) ([]ScenarioHistoryEntry, error) {
	if !isSafeID(id) {
		return nil, ErrInvalidID
	}
	dir, err := s.layout.ScenarioHistoryDir(id)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Anchor the scenario itself — there's no point in surfacing history for
	// a scenario the caller cannot otherwise read.
	if _, err := os.Stat(s.scenarioXMLPath(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	entries, err := listScenarioHistoryEntriesFromDir(dir)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// ForkScenarioFromHistory creates a new scenario from an archived snapshot.
// meta.ID must differ from sourceID. Empty name/role/description fall back to
// the snapshot sidecar when present.
func (s *Store) ForkScenarioFromHistory(sourceID, ts string, meta ScenarioMeta) (ScenarioBody, error) {
	if !isSafeID(sourceID) || !isSafeID(meta.ID) {
		return ScenarioBody{}, ErrInvalidID
	}
	if sourceID == meta.ID {
		return ScenarioBody{}, fmt.Errorf("uistore: fork target id must differ from source %q", sourceID)
	}
	hist, err := s.GetScenarioHistory(sourceID, ts)
	if err != nil {
		return ScenarioBody{}, err
	}
	meta.Name = strings.TrimSpace(meta.Name)
	if meta.Name == "" {
		if hist.Meta.Name != "" && hist.Meta.Name != sourceID {
			meta.Name = hist.Meta.Name + "_fork"
		} else {
			meta.Name = meta.ID
		}
	}
	if meta.Description == "" && hist.Meta.Description != "" {
		meta.Description = hist.Meta.Description
	}
	if meta.Role == "" && hist.Meta.Role != "" {
		meta.Role = hist.Meta.Role
	}
	return s.PutScenario(meta, hist.XML, true)
}

// DeleteScenarioHistory removes a single archived snapshot (xml + sidecar).
// Returns ErrNotFound when the snapshot does not exist.
func (s *Store) DeleteScenarioHistory(id, ts string) error {
	if !isSafeID(id) {
		return ErrInvalidID
	}
	if !isSafeHistoryTS(ts) {
		return ErrInvalidHistoryTS
	}
	dir, err := s.layout.ScenarioHistoryDir(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	xmlErr := os.Remove(filepath.Join(dir, ts+".xml"))
	metaErr := os.Remove(filepath.Join(dir, ts+".meta.json"))
	if xmlErr != nil && !errors.Is(xmlErr, os.ErrNotExist) {
		return xmlErr
	}
	if metaErr != nil && !errors.Is(metaErr, os.ErrNotExist) {
		return metaErr
	}
	if errors.Is(xmlErr, os.ErrNotExist) && errors.Is(metaErr, os.ErrNotExist) {
		return ErrNotFound
	}
	return nil
}

// GetScenarioHistory returns the archived XML+meta for a specific snapshot.
func (s *Store) GetScenarioHistory(id, ts string) (ScenarioBody, error) {
	if !isSafeID(id) {
		return ScenarioBody{}, ErrInvalidID
	}
	if !isSafeHistoryTS(ts) {
		return ScenarioBody{}, ErrInvalidHistoryTS
	}
	dir, err := s.layout.ScenarioHistoryDir(id)
	if err != nil {
		return ScenarioBody{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	xmlPath := filepath.Join(dir, ts+".xml")
	data, err := os.ReadFile(xmlPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ScenarioBody{}, ErrNotFound
		}
		return ScenarioBody{}, err
	}
	var meta ScenarioMeta
	if err := readJSON(filepath.Join(dir, ts+".meta.json"), &meta); err != nil && !errors.Is(err, ErrNotFound) {
		return ScenarioBody{}, err
	}
	if meta.ID == "" {
		meta.ID = id
	}
	if meta.Name == "" {
		meta.Name = id
	}
	return ScenarioBody{Meta: meta, XML: string(data)}, nil
}

// --------------- media (WAV / PCAP) ---------------

func (s *Store) mediaDirFor(kind MediaKind) (string, error) {
	switch kind {
	case MediaWav:
		return s.layout.WavDir(), nil
	case MediaPcap:
		return s.layout.PcapDir(), nil
	default:
		return "", fmt.Errorf("uistore: unsupported media kind %q", kind)
	}
}

// ListMedia returns assets of the requested kind sorted by name.
func (s *Store) ListMedia(kind MediaKind) ([]MediaAsset, error) {
	dir, err := s.mediaDirFor(kind)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]MediaAsset, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, MediaAsset{
			Kind:      kind,
			Name:      e.Name(),
			SizeBytes: info.Size(),
			ModTime:   info.ModTime().UTC(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// PutMedia stores an uploaded media file. The name must be a safe basename
// (no path separators). Returns the absolute on-disk path.
func (s *Store) PutMedia(kind MediaKind, name string, r io.Reader) (MediaAsset, error) {
	if !isSafeFileName(name) {
		return MediaAsset{}, fmt.Errorf("uistore: invalid media name %q", name)
	}
	dir, err := s.mediaDirFor(kind)
	if err != nil {
		return MediaAsset{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return MediaAsset{}, err
	}
	tmp, err := os.CreateTemp(s.layout.TempDir(), "media-*.tmp")
	if err != nil {
		return MediaAsset{}, err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	n, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		cleanup()
		return MediaAsset{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return MediaAsset{}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return MediaAsset{}, err
	}
	if err := os.Chmod(tmpPath, 0o640); err != nil {
		cleanup()
		return MediaAsset{}, err
	}
	if err := validateMediaContent(kind, tmpPath); err != nil {
		cleanup()
		return MediaAsset{}, err
	}
	target := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, target); err != nil {
		cleanup()
		return MediaAsset{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return MediaAsset{}, err
	}
	return MediaAsset{
		Kind:      kind,
		Name:      name,
		SizeBytes: n,
		ModTime:   info.ModTime().UTC(),
	}, nil
}

// MediaPath returns the absolute path for a media asset (no validation that it
// exists; useful for serving downloads via http.ServeFile).
func (s *Store) MediaPath(kind MediaKind, name string) (string, error) {
	if !isSafeFileName(name) {
		return "", fmt.Errorf("uistore: invalid media name %q", name)
	}
	dir, err := s.mediaDirFor(kind)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// DeleteMedia removes the given file. Returns ErrNotFound if missing.
func (s *Store) DeleteMedia(kind MediaKind, name string) error {
	path, err := s.MediaPath(kind, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func isSafeFileName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 255 || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\") {
		return false
	}
	return true
}

// --------------- generic list helper ---------------

func listJSON[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var v T
		if err := readJSON(filepath.Join(dir, e.Name()), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	sortByID(out)
	return out, nil
}

// sortByID orders any slice of structs that expose an exported "ID" string
// field (ServerProfile, ClientProfile, …) lexicographically.
func sortByID[T any](xs []T) {
	sort.Slice(xs, func(i, j int) bool {
		return idField(&xs[i]) < idField(&xs[j])
	})
}

// idField extracts the exported "ID" string from a struct pointer; returns ""
// when the field is missing.
func idField(p any) string {
	switch v := p.(type) {
	case *ServerProfile:
		return v.ID
	case *ClientProfile:
		return v.ID
	default:
		return ""
	}
}
