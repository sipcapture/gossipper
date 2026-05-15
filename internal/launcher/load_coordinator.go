package launcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/engine"
)

const maxDynamicLoadClients = 32

// LoadCoordinator starts additional UAC engines at runtime (POST /api/v1/clients).
type LoadCoordinator struct {
	mu sync.Mutex

	parentCtx context.Context
	parentCfg cli.Config

	dynWG *sync.WaitGroup

	usedIDs map[string]struct{}

	entries []dynamicLoadEntry
	seq     atomic.Uint64
}

type dynamicLoadEntry struct {
	id       string
	engine   *engine.Engine
	closeLog func() error
	cancel   context.CancelFunc
}

// NewLoadCoordinator constructs a coordinator. staticExtraIDs are existing composite client ids
// (must not collide with dynamically assigned ids). reservedIDs adds more blocked names (e.g. "primary").
func NewLoadCoordinator(ctx context.Context, parentMgmt cli.Config, staticExtraIDs []string, reservedIDs []string) *LoadCoordinator {
	used := map[string]struct{}{
		"primary": {},
	}
	for _, id := range reservedIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			used[id] = struct{}{}
		}
	}
	for _, id := range staticExtraIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			used[id] = struct{}{}
		}
	}
	wg := &sync.WaitGroup{}
	return &LoadCoordinator{
		parentCtx: ctx,
		parentCfg: parentMgmt,
		dynWG:     wg,
		usedIDs:   used,
	}
}

func (c *LoadCoordinator) pickID(want string) (string, error) {
	want = strings.TrimSpace(want)
	if want != "" {
		if _, dup := c.usedIDs[want]; dup {
			return "", fmt.Errorf("client id %q already in use", want)
		}
		return want, nil
	}
	for {
		id := fmt.Sprintf("dyn-%d", c.seq.Add(1))
		if _, ok := c.usedIDs[id]; !ok {
			return id, nil
		}
	}
}

// Add starts a new load engine from a JSON snippet (see cli.ApplyClientSnippetFromJSON).
func (c *LoadCoordinator) Add(wantID string, body []byte) (id string, eng *engine.Engine, err error) {
	cfg, err := cli.ApplyClientSnippetFromJSON(c.parentCfg, body, ".")
	if err != nil {
		return "", nil, err
	}
	if cfg.ServerMode {
		return "", nil, errors.New("dynamic client must be load (server mode is false)")
	}
	prepared, err := Prepare(cfg)
	if err != nil {
		return "", nil, err
	}

	c.mu.Lock()
	if len(c.entries) >= maxDynamicLoadClients {
		c.mu.Unlock()
		return "", nil, fmt.Errorf("at most %d dynamic load clients", maxDynamicLoadClients)
	}
	id, err = c.pickID(wantID)
	if err != nil {
		c.mu.Unlock()
		return "", nil, err
	}
	logger, closeLog, err := BuildEventLogger(prepared.CLIConfig, prepared.Scenario)
	if err != nil {
		c.mu.Unlock()
		return "", nil, err
	}
	prepared.EngineConfig.Log = logger
	app := engine.New(prepared.EngineConfig)
	runCtx, cancel := context.WithCancel(c.parentCtx)
	c.usedIDs[id] = struct{}{}
	c.entries = append(c.entries, dynamicLoadEntry{id: id, engine: app, closeLog: closeLog, cancel: cancel})
	c.mu.Unlock()

	runID := id
	c.dynWG.Add(1)
	go func() {
		defer c.dynWG.Done()
		defer func() {
			c.mu.Lock()
			delete(c.usedIDs, runID)
			c.mu.Unlock()
		}()
		_ = app.Run(runCtx)
		if closeLog != nil {
			_ = closeLog()
		}
	}()
	return id, app, nil
}

// Remove stops a dynamic client by id (cancel Run context). No-op if id is unknown.
func (c *LoadCoordinator) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("client id is required")
	}
	c.mu.Lock()
	var cancel context.CancelFunc
	found := false
	for i := range c.entries {
		if c.entries[i].id == id {
			cancel = c.entries[i].cancel
			c.entries = append(c.entries[:i], c.entries[i+1:]...)
			found = true
			break
		}
	}
	c.mu.Unlock()
	if !found {
		return fmt.Errorf("no dynamic client id %q", id)
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

// SnapshotDynamic returns engines and ids for stats/control (dynamic only).
func (c *LoadCoordinator) SnapshotDynamic() ([]*engine.Engine, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	outE := make([]*engine.Engine, 0, len(c.entries))
	outID := make([]string, 0, len(c.entries))
	for _, e := range c.entries {
		if e.engine != nil {
			outE = append(outE, e.engine)
			outID = append(outID, e.id)
		}
	}
	return outE, outID
}

// Wait blocks until all dynamic client Run goroutines finish (parent ctx cancelled).
func (c *LoadCoordinator) Wait() {
	c.dynWG.Wait()
}
