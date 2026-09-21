package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipcapture/gossipper/internal/scenario"
	"github.com/sipcapture/gossipper/internal/sip"
)

var (
	ErrNotFound     = errors.New("gateway profile not found")
	ErrDuplicateID  = errors.New("gateway profile id already exists")
	ErrInvalidID    = errors.New("invalid gateway profile id")
	ErrUseOriginate = errors.New("use originate for UAC lab scenarios")
)

// LiveEngine is the UAS engine surface used for inbound arm and OPTIONS keep-alive.
type LiveEngine interface {
	TryReplaceLiveScenario(next scenario.Scenario) error
	SetAutoAnswerOPTIONS(v bool)
	ScenarioForCall() scenario.Scenario
	SetInviteScenarioResolver(r func(requestUser string) (scenario.Scenario, bool))
}

// InviteScenarioResolver maps a Request-URI user to an armed UAS scenario.
type InviteScenarioResolver func(requestUser string) (scenario.Scenario, bool)

type runtime struct {
	cfg     Config
	reg     *Registrar
	cancel  context.CancelFunc
	done    chan struct{}
	armedID string
	armed   scenario.Scenario
}

// Controller owns gateway profiles: persist, REGISTER loops, per-AOR armed UAS.
type Controller struct {
	mu                 sync.Mutex
	restartMu          sync.Mutex
	order              []string
	runtimes           map[string]*runtime
	dataDir            string
	parent             context.Context
	engine             LiveEngine
	regIO              RegisterTransport
	defaultContactPort int
}

// NewController constructs a controller. seed may be empty (no profiles yet).
func NewController(parent context.Context, seed Config, dataDir string) *Controller {
	c := &Controller{
		runtimes: make(map[string]*runtime),
		dataDir:  dataDir,
		parent:   parent,
	}
	if seed.Domain != "" || seed.Addr != "" || seed.Username != "" || seed.ID != "" {
		seed.Enabled = true
		EnsureIdentity(&seed)
		c.upsertLocked(seed)
	}
	return c
}

// NewControllerProfiles starts with an explicit profile list (persist / CLI array).
func NewControllerProfiles(parent context.Context, seed []Config, dataDir string) *Controller {
	c := &Controller{
		runtimes: make(map[string]*runtime),
		dataDir:  dataDir,
		parent:   parent,
	}
	for i := range seed {
		cfg := seed[i]
		EnsureIdentity(&cfg)
		c.upsertLocked(cfg)
	}
	return c
}

func (c *Controller) upsertLocked(cfg Config) {
	rt, ok := c.runtimes[cfg.ID]
	if !ok {
		rt = &runtime{}
		c.runtimes[cfg.ID] = rt
		c.order = append(c.order, cfg.ID)
	}
	if c.defaultContactPort > 0 {
		cfg.ContactPort = c.defaultContactPort
	}
	rt.cfg = cfg
	if aid := strings.TrimSpace(cfg.ArmedScenarioID); aid != "" {
		rt.armedID = aid
	} else if strings.TrimSpace(rt.armedID) == "" {
		rt.armedID = "uas"
	}
}

// SetDefaultContactPort pins every profile Contact to the UAS listen port.
func (c *Controller) SetDefaultContactPort(port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.defaultContactPort = port
	if port <= 0 {
		return
	}
	for _, rt := range c.runtimes {
		rt.cfg.ContactPort = port
	}
}

// AttachEngine wires the management UAS engine (OPTIONS + live scenario swap + per-AOR resolver).
func (c *Controller) AttachEngine(eng LiveEngine) {
	c.mu.Lock()
	c.engine = eng
	c.regIO = nil
	if rt, ok := eng.(RegisterTransport); ok {
		c.regIO = rt
	}
	c.mu.Unlock()
	if eng == nil {
		return
	}
	eng.SetInviteScenarioResolver(c.ResolveInvite)
	c.syncEngineOptions()
	c.restoreArmedProfiles()
}

// ResolveInvite returns the armed UAS scenario for a Request-URI user (enabled profiles only).
func (c *Controller) ResolveInvite(requestUser string) (scenario.Scenario, bool) {
	user := strings.TrimSpace(requestUser)
	if user == "" {
		return scenario.Scenario{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.order {
		rt := c.runtimes[id]
		if rt == nil || !rt.cfg.Enabled {
			continue
		}
		if rt.cfg.AORUser() != user {
			continue
		}
		if len(rt.armed.Commands) == 0 {
			return scenario.Scenario{}, false
		}
		return rt.armed, true
	}
	return scenario.Scenario{}, false
}

// Start launches REGISTER loops for profiles that ShouldRegister.
func (c *Controller) Start() {
	c.mu.Lock()
	ids := append([]string(nil), c.order...)
	parent := c.parent
	c.mu.Unlock()
	for _, id := range ids {
		c.restartOne(parent, id)
	}
	c.syncEngineOptions()
	c.restoreArmedProfiles()
}

func (c *Controller) restartOne(parent context.Context, id string) {
	c.restartMu.Lock()
	defer c.restartMu.Unlock()

	c.mu.Lock()
	rt := c.runtimes[id]
	if rt == nil {
		c.mu.Unlock()
		return
	}
	var oldDone chan struct{}
	if rt.cancel != nil {
		rt.cancel()
		oldDone = rt.done
		rt.cancel = nil
		rt.reg = nil
		rt.done = nil
	}
	cfg := rt.cfg
	trip := c.regIO
	c.mu.Unlock()

	if oldDone != nil {
		select {
		case <-oldDone:
		case <-time.After(defaultDeregisterTO + time.Second):
		}
	}
	if !cfg.ShouldRegister() {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	reg := NewRegistrar(cfg)
	reg.SetTransport(trip)
	done := make(chan struct{})
	c.mu.Lock()
	if cur := c.runtimes[id]; cur != nil {
		cur.cancel = cancel
		cur.reg = reg
		cur.done = done
	} else {
		cancel()
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	go func() {
		defer close(done)
		reg.Loop(ctx)
	}()
}

func (c *Controller) syncEngineOptions() {
	c.mu.Lock()
	eng := c.engine
	anyOn := c.anyEnabledLocked()
	adv := c.firstEnabledAdvertisedIPLocked()
	c.mu.Unlock()
	if eng == nil {
		return
	}
	eng.SetAutoAnswerOPTIONS(anyOn)
	if s, ok := eng.(interface{ SetAdvertisedIP(string) }); ok {
		s.SetAdvertisedIP(adv)
	}
}

func (c *Controller) firstEnabledAdvertisedIPLocked() string {
	for _, id := range c.order {
		rt := c.runtimes[id]
		if rt == nil || !rt.cfg.Enabled {
			continue
		}
		if ip := strings.TrimSpace(rt.cfg.AdvertisedIP); ip != "" {
			return ip
		}
	}
	return ""
}

func (c *Controller) anyEnabledLocked() bool {
	for _, rt := range c.runtimes {
		if rt.cfg.Enabled {
			return true
		}
	}
	return false
}

func (c *Controller) firstIDLocked() string {
	if len(c.order) == 0 {
		return ""
	}
	return c.order[0]
}

// Config returns the first profile (launcher / CLI seed). Empty if none.
func (c *Controller) Config() Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.firstIDLocked()
	if id == "" {
		return Config{}
	}
	return c.runtimes[id].cfg
}

// ArmedID is the inbound UAS scenario id of the first profile.
func (c *Controller) ArmedID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.firstIDLocked()
	if id == "" {
		return "uas"
	}
	if a := c.runtimes[id].armedID; a != "" {
		return a
	}
	return "uas"
}

// Snapshot is GET /gateway (first profile, or empty off).
func (c *Controller) Snapshot() Snapshot {
	c.mu.Lock()
	id := c.firstIDLocked()
	c.mu.Unlock()
	if id == "" {
		return Snapshot{Config: Config{}.Masked(), Status: State{State: StateOff}, ArmedScenarioID: "uas"}
	}
	snap, err := c.Get(id)
	if err != nil {
		return Snapshot{Config: Config{}.Masked(), Status: State{State: StateOff}, ArmedScenarioID: "uas"}
	}
	return snap
}

// List returns snapshots in persist order.
func (c *Controller) List() []Snapshot {
	c.mu.Lock()
	ids := append([]string(nil), c.order...)
	c.mu.Unlock()
	out := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		snap, err := c.Get(id)
		if err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out
}

// Get returns one profile snapshot.
func (c *Controller) Get(id string) (Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rt := c.runtimes[strings.TrimSpace(id)]
	if rt == nil {
		return Snapshot{}, ErrNotFound
	}
	armed := rt.armedID
	if armed == "" {
		armed = "uas"
	}
	st := State{State: StateOff, AOR: rt.cfg.AOR()}
	if rt.reg != nil {
		st = rt.reg.Snapshot()
	} else if !rt.cfg.ShouldRegister() {
		st.State = StateOff
	}
	return Snapshot{Config: rt.cfg.Masked(), Status: st, ArmedScenarioID: armed}, nil
}

// Snapshot is the public GET body.
type Snapshot struct {
	Config          Config `json:"config"`
	Status          State  `json:"status"`
	ArmedScenarioID string `json:"armed_scenario_id"`
}

func (c *Controller) persistLocked() error {
	if c.dataDir == "" {
		return nil
	}
	profiles := make([]Config, 0, len(c.order))
	for _, id := range c.order {
		rt := c.runtimes[id]
		if rt == nil {
			continue
		}
		cfg := rt.cfg
		if aid := strings.TrimSpace(rt.armedID); aid != "" {
			cfg.ArmedScenarioID = aid
			rt.cfg.ArmedScenarioID = aid
		}
		profiles = append(profiles, cfg)
	}
	return SavePersist(c.dataDir, profiles)
}

// Create adds a profile and starts REGISTER when enabled.
func (c *Controller) Create(next Config) (Snapshot, error) {
	next.Normalize()
	if next.ID == "" {
		next.ID = NewProfileID()
	}
	if !ValidProfileID(next.ID) {
		return Snapshot{}, ErrInvalidID
	}
	if next.Name == "" {
		EnsureIdentity(&next)
	}
	c.mu.Lock()
	if _, exists := c.runtimes[next.ID]; exists {
		c.mu.Unlock()
		return Snapshot{}, ErrDuplicateID
	}
	c.upsertLocked(next)
	if err := c.persistLocked(); err != nil {
		delete(c.runtimes, next.ID)
		c.order = c.order[:len(c.order)-1]
		c.mu.Unlock()
		return Snapshot{}, err
	}
	parent := c.parent
	id := next.ID
	c.mu.Unlock()
	c.restartOne(parent, id)
	c.syncEngineOptions()
	if next.Enabled {
		armID := strings.TrimSpace(next.ArmedScenarioID)
		if armID == "" {
			armID = "uas"
		}
		_ = c.ArmProfile(id, armID)
	}
	return c.Get(id)
}

// Update replaces one profile (password merge) and restarts its REGISTER loop.
func (c *Controller) Update(id string, next Config) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	rt := c.runtimes[id]
	if rt == nil {
		c.mu.Unlock()
		return ErrNotFound
	}
	prev := rt.cfg
	keepArmed := rt.armedID
	c.mu.Unlock()
	next = MergePassword(prev, next)
	next.ID = id
	next.Normalize()
	if next.Name == "" {
		next.Name = prev.Name
	}
	if next.ContactPort <= 0 {
		next.ContactPort = prev.ContactPort
	}
	if strings.TrimSpace(next.ArmedScenarioID) == "" {
		next.ArmedScenarioID = keepArmed
	}
	if strings.TrimSpace(next.OriginateScenarioID) == "" {
		next.OriginateScenarioID = prev.OriginateScenarioID
	}
	if strings.TrimSpace(next.OriginateTo) == "" {
		next.OriginateTo = prev.OriginateTo
	}
	if next.OriginateCalls <= 0 {
		next.OriginateCalls = prev.OriginateCalls
	}
	needRestart := !registerWireEqual(prev, next)
	c.mu.Lock()
	c.upsertLocked(next)
	err := c.persistLocked()
	armed := rt.armedID
	parent := c.parent
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if needRestart {
		c.restartOne(parent, id)
	}
	c.syncEngineOptions()
	if next.Enabled {
		if armed == "" || armed == "management" {
			armed = "uas"
		}
		_ = c.ArmProfile(id, armed)
	}
	return nil
}

func registerWireEqual(a, b Config) bool {
	return a.Enabled == b.Enabled &&
		a.Register == b.Register &&
		a.Domain == b.Domain &&
		a.Addr == b.Addr &&
		a.Transport == b.Transport &&
		a.Username == b.Username &&
		a.Password == b.Password &&
		a.RegisterUser == b.RegisterUser &&
		a.AdvertisedIP == b.AdvertisedIP &&
		a.RegisterExpires == b.RegisterExpires &&
		a.KeepaliveSeconds == b.KeepaliveSeconds &&
		a.ContactPort == b.ContactPort
}

// UpdateFirst is PUT /gateway: create if empty, else update the first profile.
func (c *Controller) UpdateFirst(next Config) error {
	c.mu.Lock()
	id := c.firstIDLocked()
	c.mu.Unlock()
	if id == "" {
		_, err := c.Create(next)
		return err
	}
	return c.Update(id, next)
}

// Delete removes a profile and stops its REGISTER loop.
func (c *Controller) Delete(id string) error {
	id = strings.TrimSpace(id)
	c.mu.Lock()
	rt := c.runtimes[id]
	if rt == nil {
		c.mu.Unlock()
		return ErrNotFound
	}
	if rt.cancel != nil {
		rt.cancel()
	}
	delete(c.runtimes, id)
	order := c.order[:0]
	for _, existing := range c.order {
		if existing != id {
			order = append(order, existing)
		}
	}
	c.order = order
	err := c.persistLocked()
	c.mu.Unlock()
	c.syncEngineOptions()
	return err
}

// Arm swaps the live UAS scenario on the first profile (compat).
func (c *Controller) Arm(id string) error {
	c.mu.Lock()
	pid := c.firstIDLocked()
	c.mu.Unlock()
	return c.ArmProfile(pid, id)
}

// ArmProfile arms inbound UAS for one gateway id.
func (c *Controller) ArmProfile(profileID, scenarioID string) error {
	scenarioID = strings.TrimSpace(scenarioID)
	if scenarioID == "" {
		scenarioID = "uas"
	}
	return c.applyArmID(profileID, scenarioID, nil)
}

// ArmScenario applies a pre-parsed UAS scenario on the first profile (compat).
func (c *Controller) ArmScenario(id string, sc scenario.Scenario) error {
	c.mu.Lock()
	pid := c.firstIDLocked()
	c.mu.Unlock()
	return c.ArmProfileScenario(pid, id, sc)
}

// ArmProfileScenario applies a pre-parsed UAS scenario to one profile.
func (c *Controller) ArmProfileScenario(profileID, scenarioID string, sc scenario.Scenario) error {
	scenarioID = strings.TrimSpace(scenarioID)
	if scenarioID == "" {
		return errors.New("scenario_id is required")
	}
	if err := RequireUASInvite(sc, scenarioID); err != nil {
		return err
	}
	c.mu.Lock()
	eng := c.engine
	pid := strings.TrimSpace(profileID)
	rt := c.runtimes[pid]
	if pid != "" && rt == nil {
		c.mu.Unlock()
		return ErrNotFound
	}
	var persistErr error
	if rt != nil {
		rt.armedID = scenarioID
		rt.armed = sc
		rt.cfg.ArmedScenarioID = scenarioID
		persistErr = c.persistLocked()
	}
	c.mu.Unlock()
	if persistErr != nil {
		return persistErr
	}
	if eng != nil {
		if err := eng.TryReplaceLiveScenario(sc); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) restoreArmedProfiles() {
	c.mu.Lock()
	jobs := make([][2]string, 0, len(c.order))
	for _, id := range c.order {
		rt := c.runtimes[id]
		if rt == nil {
			continue
		}
		sid := strings.TrimSpace(rt.armedID)
		if sid == "" {
			sid = strings.TrimSpace(rt.cfg.ArmedScenarioID)
		}
		if sid == "" {
			sid = "uas"
		}
		jobs = append(jobs, [2]string{id, sid})
	}
	c.mu.Unlock()
	for _, j := range jobs {
		_ = c.ArmProfile(j[0], j[1])
	}
}

func (c *Controller) applyArmID(profileID, id string, storeXML func(id string) (scenario.Scenario, error)) error {
	if err := RejectUACArm(id); err != nil {
		return err
	}
	sc, err := scenario.LoadNamed(id)
	if err != nil {
		if storeXML == nil {
			return err
		}
		sc, err = storeXML(id)
		if err != nil {
			return err
		}
	}
	return c.ArmProfileScenario(profileID, id, sc)
}

// OriginateOverlay is the engine map for POST /jobs (first profile, compat).
func (c *Controller) OriginateOverlay(to string, totalCalls int) (map[string]any, error) {
	c.mu.Lock()
	id := c.firstIDLocked()
	c.mu.Unlock()
	if id == "" {
		return nil, errors.New("gateway registrar is not configured")
	}
	return c.OriginateOverlayProfile(id, to, totalCalls)
}

// OriginateOverlayProfile builds the UAC job overlay for one profile.
func (c *Controller) OriginateOverlayProfile(id, to string, totalCalls int) (map[string]any, error) {
	c.mu.Lock()
	rt := c.runtimes[strings.TrimSpace(id)]
	if rt == nil {
		c.mu.Unlock()
		return nil, ErrNotFound
	}
	cfg := rt.cfg
	c.mu.Unlock()
	if !cfg.Enabled {
		return nil, errors.New("gateway profile is disabled")
	}
	if strings.TrimSpace(cfg.Addr) == "" || strings.TrimSpace(cfg.Domain) == "" || cfg.AORUser() == "" {
		return nil, errors.New("gateway registrar is not configured")
	}
	host, port, err := SplitAddr(cfg.Addr)
	if err != nil {
		return nil, err
	}
	if totalCalls <= 0 {
		totalCalls = 1
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return nil, errors.New("to is required")
	}
	out := map[string]any{
		"remote_host":    host,
		"remote_port":    port,
		"sip_from":       cfg.SIPFrom(),
		"auth_username":  cfg.AuthUsername(),
		"auth_password":  cfg.Password,
		"service":        to,
		"total_calls":    totalCalls,
		"max_concurrent": totalCalls,
		"local_port":     0,
	}
	return out, nil
}

// RememberOriginate writes the last UAC originate defaults without touching REGISTER.
func (c *Controller) RememberOriginate(id, scenarioID, to string, calls int) error {
	id = strings.TrimSpace(id)
	scenarioID = strings.TrimSpace(scenarioID)
	to = strings.TrimSpace(to)
	if calls <= 0 {
		calls = 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if id == "" || id == "gateway" {
		id = c.firstIDLocked()
	}
	rt := c.runtimes[id]
	if rt == nil {
		return ErrNotFound
	}
	if scenarioID != "" {
		rt.cfg.OriginateScenarioID = scenarioID
	}
	rt.cfg.OriginateTo = to
	rt.cfg.OriginateCalls = calls
	return c.persistLocked()
}

// Stop cancels every REGISTER loop.
func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, rt := range c.runtimes {
		if rt.cancel != nil {
			rt.cancel()
			rt.cancel = nil
		}
	}
}

// RequestURIUser is sip.RequestURIUser for callers that already import gateway.
func RequestURIUser(uri string) string {
	return sip.RequestURIUser(uri)
}

// RejectUACArm rejects known UAC builtin/lab ids.
func RejectUACArm(id string) error {
	id = strings.TrimSpace(id)
	for _, b := range scenario.ListBuiltins() {
		if b.ID != id {
			continue
		}
		if strings.EqualFold(b.Role, "uac") {
			return fmt.Errorf("%w: %s is UAC", ErrUseOriginate, id)
		}
		if id == "management" {
			return fmt.Errorf("management XML only answers OPTIONS; arm uas or a lab UAS")
		}
		if strings.HasPrefix(id, "webrtc_") {
			return fmt.Errorf("webrtc scenarios are not used on the SIP REGISTER gateway")
		}
		return nil
	}
	return nil
}

// RequireUASInvite requires the first <recv> to be INVITE.
func RequireUASInvite(sc scenario.Scenario, id string) error {
	if sc.Mode != scenario.ModeServer {
		return fmt.Errorf("%w: scenario %s is not UAS", ErrUseOriginate, id)
	}
	for _, cmd := range sc.Commands {
		if cmd.Type != scenario.CommandRecv {
			if cmd.Type == scenario.CommandSend {
				return fmt.Errorf("%w: scenario %s starts with send (UAC)", ErrUseOriginate, id)
			}
			continue
		}
		if !strings.EqualFold(cmd.RecvReq, "INVITE") {
			return fmt.Errorf("arm requires first recv INVITE, got %q", cmd.RecvReq)
		}
		return nil
	}
	return fmt.Errorf("scenario %s has no recv INVITE", id)
}

// RequireUACOriginate requires a UAC (client) scenario for originate.
func RequireUACOriginate(id string, sc scenario.Scenario) error {
	id = strings.TrimSpace(id)
	for _, b := range scenario.ListBuiltins() {
		if b.ID == id && strings.EqualFold(b.Role, "uas") {
			return fmt.Errorf("scenario %s is UAS; use PUT /gateway/arm for inbound", id)
		}
	}
	if sc.Mode == scenario.ModeServer {
		return fmt.Errorf("scenario %s is UAS; use PUT /gateway/arm for inbound", id)
	}
	return nil
}
