package cli

import (
	"testing"
)

func TestTryLoadCompositeFlatJSONMergesRootAPIAddr(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "api_addr": ":18080",
  "server": {"id": "sip", "role": "management", "scenario_name": "management", "transport": "u1", "local_ip": "127.0.0.1", "local_port": 50660},
  "clients": [
    {"id": "load", "role": "load", "scenario_name": "uac", "transport": "u1", "local_ip": "127.0.0.1", "local_port": 50661, "remote_addr": "127.0.0.1:50660", "total_calls": 1, "rate": 1, "max_concurrent": 1}
  ]
}`)
	p, joined, _, ok, err := TryLoadCompositeFlatJSON("t.json", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected composite")
	}
	if p.ApiAddr != ":18080" {
		t.Fatalf("primary ApiAddr=%q joined=%d", p.ApiAddr, len(joined))
	}
}

func TestTryLoadCompositeRejectsWorkloadsKey(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"api_addr": ":8080", "workloads": [{"id": "x"}]}`)
	_, _, _, ok, err := TryLoadCompositeFlatJSON("t.json", raw)
	if err == nil {
		t.Fatal("expected error")
	}
	if ok {
		t.Fatal("expected not composite ok")
	}
}
