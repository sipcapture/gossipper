package pcap2scenario

import (
	"fmt"
	"os"
	"path/filepath"
)

// Run is the top-level entry point for the pcap2scenario command.  It:
//  1. Extracts SIP messages and UDP packets from pcapPath.
//  2. Reconstructs the SIP dialog.
//  3. Writes caller_rtp.pcap and callee_rtp.pcap into outDir.
//  4. Generates scenario_uac.xml and scenario_uas.xml in outDir.
func Run(pcapPath, outDir string, sipPort int, linkSpec string) error {
	// ── 1. Extract ────────────────────────────────────────────────────────
	fmt.Printf("pcap2scenario: reading %s ...\n", pcapPath)
	result, err := Extract(pcapPath, sipPort, linkSpec)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}
	fmt.Printf("  %d SIP messages, %d UDP packets\n",
		len(result.SIPMessages), len(result.UDPPackets))

	// ── 2. Build dialog ───────────────────────────────────────────────────
	dlg, err := BuildDialog(result.SIPMessages)
	if err != nil {
		return fmt.Errorf("build dialog: %w", err)
	}
	fmt.Printf("  Call-ID: %s\n", dlg.CallID)
	fmt.Printf("  caller: %s:%d  rtp=%s:%d\n",
		dlg.CallerIP, dlg.CallerSIPPort, dlg.CallerRTPIP, dlg.CallerRTPPort)
	fmt.Printf("  callee: %s:%d  rtp=%s:%d\n",
		dlg.CalleeIP, dlg.CalleeSIPPort, dlg.CalleeRTPIP, dlg.CalleeRTPPort)
	fmt.Printf("  call duration: %s\n", dlg.CallDuration)

	// ── 3. Create output directory ────────────────────────────────────────
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	// ── 4. Split RTP ──────────────────────────────────────────────────────
	callerRTPPath := filepath.Join(outDir, "caller_rtp.pcap")
	calleeRTPPath := filepath.Join(outDir, "callee_rtp.pcap")
	if err := SplitRTP(result, dlg, callerRTPPath, calleeRTPPath); err != nil {
		return err
	}

	// ── 5. Generate scenario XMLs ─────────────────────────────────────────
	pcapBase := filepath.Base(pcapPath)
	uacXML, uasXML := GenerateScenarios(dlg, "caller_rtp.pcap", "callee_rtp.pcap", pcapBase)

	uacPath := filepath.Join(outDir, "scenario_uac.xml")
	uasPath := filepath.Join(outDir, "scenario_uas.xml")

	if err := os.WriteFile(uacPath, []byte(uacXML), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", uacPath, err)
	}
	if err := os.WriteFile(uasPath, []byte(uasXML), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", uasPath, err)
	}

	fmt.Println("\nGenerated:")
	fmt.Printf("  %s\n", uacPath)
	fmt.Printf("  %s\n", uasPath)
	fmt.Printf("  %s\n", callerRTPPath)
	fmt.Printf("  %s\n", calleeRTPPath)
	return nil
}
