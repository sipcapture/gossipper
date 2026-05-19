package webrtc

import "testing"

func TestBridgeRTPStatsEmpty(t *testing.T) {
	t.Parallel()
	b, err := NewBridge(Options{PrefersPCMA: true})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	st := b.RTPStats()
	if st.PacketsSent != 0 || st.PacketsReceived != 0 {
		t.Fatalf("expected zero stats on fresh bridge, got %+v", st)
	}
}
