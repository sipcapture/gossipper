# Kefir lab scenarios (UAC/UAS XML)

Ports of the kefir bundled lab catalog (`internal/siprtp/bundled/*.toml`) into
Gossipper SIPp XML. Same ids. Run with `-sn <id>` or clone from Control UI
Scenarios → **Lab (kefir)**.

Gossipper is not kefir: there is no `drop_rx` / hairpin socket / `kws.ulaw` /
illegal post-CONNECT 180 on a UAC. Approximations are noted in each file’s
header comment and in [docs/lab-scenarios.md](../../../docs/lab-scenarios.md).
