package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sipcapture/gossipper/internal/scenario"
)

type fakeEngine struct {
	armed   scenario.Scenario
	options bool
}

func (f *fakeEngine) TryReplaceLiveScenario(next scenario.Scenario) error {
	f.armed = next
	return nil
}
func (f *fakeEngine) SetAutoAnswerOPTIONS(v bool) { f.options = v }
func (f *fakeEngine) ScenarioForCall() scenario.Scenario {
	return f.armed
}
func (f *fakeEngine) SetInviteScenarioResolver(_ func(string) (scenario.Scenario, bool)) {}

type fakeAdvEngine struct {
	fakeEngine
	advertised string
}

func (f *fakeAdvEngine) SetAdvertisedIP(ip string) { f.advertised = ip }

func TestRejectUACArm(t *testing.T) {
	t.Parallel()
	if err := RejectUACArm("one_way"); err == nil || !errors.Is(err, ErrUseOriginate) {
		t.Fatalf("one_way: %v", err)
	}
	if err := RejectUACArm("uas"); err != nil {
		t.Fatalf("uas: %v", err)
	}
	if err := RejectUACArm("early_183"); err != nil {
		t.Fatalf("early_183: %v", err)
	}
	if err := RejectUACArm("one_way_uas"); err != nil {
		t.Fatalf("one_way_uas: %v", err)
	}
	if err := RejectUACArm("management"); err == nil {
		t.Fatal("expected management reject")
	}
}

func TestArmRejectsUACAndAppliesUAS(t *testing.T) {
	t.Parallel()
	c := NewController(context.Background(), Config{ID: "gw-test", Name: "test"}, t.TempDir())
	eng := &fakeEngine{}
	c.AttachEngine(eng)
	if err := c.Arm("one_way"); err == nil {
		t.Fatal("expected UAC arm error")
	}
	if err := c.Arm("early_183"); err != nil {
		t.Fatal(err)
	}
	if c.ArmedID() != "early_183" {
		t.Fatalf("armed=%s", c.ArmedID())
	}
	if !strings.EqualFold(eng.armed.Name, "early_183") && eng.armed.Name == "" {
		// Name may be the XML scenario name; first recv must still be INVITE.
	}
	if err := RequireUASInvite(eng.armed, "early_183"); err != nil {
		t.Fatal(err)
	}
	if err := c.Arm("one_way_uas"); err != nil {
		t.Fatal(err)
	}
	if c.ArmedID() != "one_way_uas" {
		t.Fatalf("armed=%s", c.ArmedID())
	}
	if err := RequireUASInvite(eng.armed, "one_way_uas"); err != nil {
		t.Fatal(err)
	}
}

func TestOriginateOverlaySipFromAndRemote(t *testing.T) {
	t.Parallel()
	c := NewController(context.Background(), Config{
		Domain:   "pbx.local",
		Addr:     "192.168.1.10:5060",
		Username: "1001",
		Password: "secret",
		Register: true,
	}, "")
	ov, err := c.OriginateOverlay("100", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ov["remote_host"] != "192.168.1.10" {
		t.Fatalf("remote_host=%v", ov["remote_host"])
	}
	if ov["remote_port"] != 5060 {
		t.Fatalf("remote_port=%v", ov["remote_port"])
	}
	if ov["sip_from"] != "sip:1001@pbx.local" {
		t.Fatalf("sip_from=%v", ov["sip_from"])
	}
	if ov["auth_username"] != "1001" || ov["auth_password"] != "secret" {
		t.Fatalf("auth=%v / %v", ov["auth_username"], ov["auth_password"])
	}
	if ov["service"] != "100" {
		t.Fatalf("service=%v", ov["service"])
	}
}

func TestPersistRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "1001", Password: "s3cret", Register: true}
	if err := SavePersist(dir, []Config{cfg}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadPersist(dir)
	if err != nil || !ok {
		t.Fatalf("load ok=%v err=%v", ok, err)
	}
	if len(got) != 1 || got[0].Password != "s3cret" || got[0].Username != "1001" {
		t.Fatalf("got %+v", got)
	}
	masked := got[0].Masked()
	if masked.Password != "***" {
		t.Fatalf("masked=%q", masked.Password)
	}
}

func TestArmScenarioPersistsAndReloads(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		ID: "gw-a", Name: "Botauro", Enabled: true,
		Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "160",
	}
	c := NewControllerProfiles(context.Background(), []Config{cfg}, dir)
	sc, err := scenario.LoadNamed("fake_ringing_uas")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArmProfileScenario("gw-a", "fake_ringing_uas", sc); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadPersist(dir)
	if err != nil || !ok || len(loaded) != 1 {
		t.Fatalf("load ok=%v err=%v n=%d", ok, err, len(loaded))
	}
	if loaded[0].ArmedScenarioID != "fake_ringing_uas" {
		t.Fatalf("persist armed=%q", loaded[0].ArmedScenarioID)
	}

	save := loaded[0]
	save.Name = "Renamed"
	save.ArmedScenarioID = ""
	if err := c.Update("gw-a", save); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err = LoadPersist(dir)
	if err != nil || !ok || loaded[0].ArmedScenarioID != "fake_ringing_uas" {
		t.Fatalf("save must keep arm: ok=%v err=%v armed=%q", ok, err, loaded[0].ArmedScenarioID)
	}

	c2 := NewControllerProfiles(context.Background(), loaded, dir)
	c2.Start()
	t.Cleanup(c2.Stop)
	got, ok := c2.ResolveInvite("160")
	if !ok || got.Name != sc.Name {
		t.Fatalf("reload resolve ok=%v name=%q want %q", ok, got.Name, sc.Name)
	}
	if c2.ArmedID() != "fake_ringing_uas" {
		t.Fatalf("ArmedID=%q", c2.ArmedID())
	}
}

func TestUpdateOriginateDefaultsPersist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		ID: "gw-a", Name: "Botauro", Enabled: true,
		Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "160",
	}
	c := NewControllerProfiles(context.Background(), []Config{cfg}, dir)
	save := cfg
	save.Password = ""
	save.OriginateScenarioID = "short_call"
	save.OriginateTo = "100"
	save.OriginateCalls = 3
	if err := c.Update("gw-a", save); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadPersist(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if loaded[0].OriginateScenarioID != "short_call" || loaded[0].OriginateTo != "100" || loaded[0].OriginateCalls != 3 {
		t.Fatalf("originate %+v", loaded[0])
	}
	keep := cfg
	keep.Password = ""
	if err := c.Update("gw-a", keep); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err = LoadPersist(dir)
	if err != nil || !ok || loaded[0].OriginateScenarioID != "short_call" || loaded[0].OriginateTo != "100" {
		t.Fatalf("empty PUT dropped orig: ok=%v err=%v %+v", ok, err, loaded[0])
	}
}

func TestRememberOriginatePersists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		ID: "gw-a", Name: "Botauro", Enabled: true,
		Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "160",
	}
	c := NewControllerProfiles(context.Background(), []Config{cfg}, dir)
	if err := c.RememberOriginate("gw-a", "one_way", "145", 2); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadPersist(dir)
	if err != nil || !ok || loaded[0].OriginateScenarioID != "one_way" || loaded[0].OriginateTo != "145" || loaded[0].OriginateCalls != 2 {
		t.Fatalf("remember: ok=%v err=%v %+v", ok, err, loaded[0])
	}
}

func TestUpdateArmedScenarioIDInBodyPersists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := Config{
		ID: "gw-a", Name: "Botauro", Enabled: true,
		Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "160",
	}
	c := NewControllerProfiles(context.Background(), []Config{cfg}, dir)
	save := cfg
	save.Password = ""
	save.ArmedScenarioID = "fake_ringing_uas"
	if err := c.Update("gw-a", save); err != nil {
		t.Fatal(err)
	}
	loaded, ok, err := LoadPersist(dir)
	if err != nil || !ok || loaded[0].ArmedScenarioID != "fake_ringing_uas" {
		t.Fatalf("save body arm: ok=%v err=%v armed=%q", ok, err, loaded[0].ArmedScenarioID)
	}
	if c.ArmedID() != "fake_ringing_uas" {
		t.Fatalf("ArmedID=%q", c.ArmedID())
	}
}

func TestMultiProfileResolveInviteAndDisable(t *testing.T) {
	t.Parallel()
	c := NewControllerProfiles(context.Background(), []Config{
		{ID: "gw-a", Name: "A", Enabled: true, Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "1001"},
		{ID: "gw-b", Name: "B", Enabled: true, Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "1002"},
	}, "")
	early, err := scenario.LoadNamed("early_183")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ArmProfileScenario("gw-b", "early_183", early); err != nil {
		t.Fatal(err)
	}
	got, ok := c.ResolveInvite("1002")
	if !ok || got.Name != early.Name {
		t.Fatalf("1002 ok=%v name=%q", ok, got.Name)
	}
	if _, ok := c.ResolveInvite("1001"); ok {
		t.Fatal("1001 should not resolve until armed")
	}
	if err := c.Update("gw-b", Config{
		Name: "B", Enabled: false, Domain: "pbx.local", Addr: "10.0.0.1:5060", Username: "1002",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.ResolveInvite("1002"); ok {
		t.Fatal("disabled profile must not resolve")
	}
	if _, err := c.OriginateOverlayProfile("gw-b", "100", 1); err == nil {
		t.Fatal("expected disabled originate error")
	}
	list := c.List()
	if len(list) != 2 {
		t.Fatalf("list=%d", len(list))
	}
}

func TestUpdateNameDoesNotDeregister(t *testing.T) {
	stub := startStubRegistrar(t, stubMode{expires: 60})
	cfg := Config{
		ID: "gw-a", Name: "A", Enabled: true,
		Domain: "pbx.local", Addr: stub.addr,
		Username: "1001", Password: "secret",
		Register: true, AdvertisedIP: "127.0.0.1", ContactPort: 5060,
		RegisterExpires: 60,
	}
	c := NewController(context.Background(), cfg, t.TempDir())
	c.Start()
	t.Cleanup(c.Stop)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := c.Get("gw-a")
		if err == nil && snap.Status.State == StateRegistered {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	n := stub.count()
	if n < 1 {
		t.Fatal("expected REGISTER before name update")
	}
	next := cfg
	next.Name = "Renamed desk"
	if err := c.Update("gw-a", next); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if stub.count() != n {
		t.Fatalf("name-only update restarted REGISTER (%d -> %d)", n, stub.count())
	}
	stub.mu.Lock()
	exp0 := stub.exp0
	stub.mu.Unlock()
	if exp0 {
		t.Fatal("name-only update sent Expires: 0")
	}
}

func TestAttachEnginePushesAdvertisedIP(t *testing.T) {
	t.Parallel()
	c := NewController(context.Background(), Config{
		ID:           "gw-nat",
		Enabled:      true,
		AdvertisedIP: "84.186.224.78",
	}, t.TempDir())
	eng := &fakeAdvEngine{}
	c.AttachEngine(eng)
	if eng.advertised != "84.186.224.78" {
		t.Fatalf("advertised=%q", eng.advertised)
	}
	next := c.Config()
	next.Enabled = false
	if err := c.Update(next.ID, next); err != nil {
		t.Fatal(err)
	}
	if eng.advertised != "" {
		t.Fatalf("disabled should clear advertised, got %q", eng.advertised)
	}
}
