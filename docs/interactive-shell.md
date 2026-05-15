# Interactive shell (`gossipper shell` / `gossipper cli`)

The line-oriented shell accumulates the same CLI flags as the main binary, then runs the SIP engine with **`run`**. Useful when tuning parameters without long one-liners.

## Launch

```bash
gossipper shell
# or
gossipper cli
```

The **`sipp`** prefix is **not** supported here — use **`gossipper shell`** / **`gossipper cli`** directly (**`gossipper sipp shell`** is rejected; see [`cli.md`](cli.md)).

## Commands (summary)

| Command | Purpose |
|---------|---------|
| `help` | Command list |
| `wizard` | Guided prompts for a minimal UAC or UAS profile |
| `hint` | Suggestions for missing or risky flags from the current session |
| `set <flag> [value]` | Set a flag (readable names: `destination`, `builtin_scenario`, `local_bind_ip`, `destination_host` + `destination_port`, …; short `rsa` / `sn` / `i` still work) |
| `set` | With no args: short cheatsheet of common flags |
| `unset <flag>` | Remove a flag from the session |
| `show` | Print effective argv and values |
| `parse` / `check` | `cli.Parse` + `launcher.Prepare` only (no traffic) |
| `run` | Execute the scenario (Ctrl+C to stop) |
| `reset` | Clear all session flags |
| `quit` / `exit` | Leave the shell |

Lines starting with `#` are comments.

## Example: UAC then run

```text
gossipper shell — type help, hint, or wizard. Ctrl+D or quit to exit.
gossipper> hint
Hints for the current session:
  • Session is empty. Quick start: wizard   or at least: set builtin_scenario uac + set destination HOST:PORT + set local_bind_ip LOCAL_IP
gossipper> set builtin_scenario uac
ok: -sn
gossipper> set destination 127.0.0.1:5060
ok: -rsa
gossipper> set local_bind_ip 127.0.0.1
ok: -i
gossipper> set listen_port 5060
ok: -p
gossipper> set total_calls 1
ok: -m
gossipper> set stat_period 5s
ok: -stat_period
gossipper> parse
parse ok
gossipper> run
running… (Ctrl+C to stop)
…
run finished
```

## Example: wizard

```text
gossipper> wizard
=== gossipper quick wizard ===
…
Wizard done. Review: show   suggestions: hint   then: run
gossipper> show
gossipper> run
```

## Full-screen TUI

For a graphical form and live dashboard instead of a REPL, use:

```bash
gossipper tui
# or
gossipper -interactive
```

See **`docs/tui.md`** for TUI-specific behaviour.

## Help from the binary

```bash
gossipper -h
```

The preamble lists **`shell`**, **`cli`**, **`tui`**, and **`-interactive`**, then points to **`gossipper sipp -h`** (full SIPp-compat flags) and **`gossipper server -h`** (management-oriented groups) — no long scenario list on **`gossipper -h`** alone.
