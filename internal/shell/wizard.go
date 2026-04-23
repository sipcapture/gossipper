package shell

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func readLine(in *bufio.Reader, out io.Writer, prompt, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, def)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}
	line, err := in.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// RunWizard interactively fills the session for a basic UAC or UAS test.
func RunWizard(in *bufio.Reader, out, errOut io.Writer, s *Session) error {
	fmt.Fprintln(out, "=== gossipper quick wizard ===")
	fmt.Fprintln(out, "Answer prompts; press Enter to accept [defaults]. Ctrl+C to abort.")
	fmt.Fprintln(out)

	role, err := readLine(in, out, "Role: uac (client) or uas (server)", "uac")
	if err != nil {
		return err
	}
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "uac", "client", "c":
		role = "uac"
	case "uas", "server", "s":
		role = "uas"
	default:
		fmt.Fprintf(errOut, "unknown role %q, using uac\n", role)
		role = "uac"
	}
	if err := s.Set("builtin_scenario", role); err != nil {
		return err
	}

	if role == "uac" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "UAC: sends INVITE to the remote peer. Use the real host:port that listens for SIP.")
		rsa, err := readLine(in, out, "Remote SIP address (host:port, maps to -rsa)", "")
		if err != nil {
			return err
		}
		if rsa == "" {
			fmt.Fprintln(errOut, "remote address is required for UAC")
			return fmt.Errorf("wizard: missing destination (host:port)")
		}
		if err := s.Set("destination", rsa); err != nil {
			return err
		}

		fmt.Fprintln(out, "Local IP in Via/Contact: avoid 0.0.0.0 on multi-host UDP (use this machine's routable IP).")
		ip, err := readLine(in, out, "Local bind IP (local_bind_ip / -i)", "127.0.0.1")
		if err != nil {
			return err
		}
		if ip != "" {
			if err := s.Set("local_bind_ip", ip); err != nil {
				return err
			}
		}

		p, err := readLine(in, out, "Local UDP port (listen_port / -p)", "5060")
		if err != nil {
			return err
		}
		if p != "" {
			if err := s.Set("listen_port", p); err != nil {
				return err
			}
		}

		t, err := readLine(in, out, "Transport (transport / -t): u1 un ui t1 tn l1 ln", "u1")
		if err != nil {
			return err
		}
		if t != "" {
			if err := s.Set("transport", t); err != nil {
				return err
			}
		}

		m, err := readLine(in, out, "Total calls (total_calls / -m)", "1")
		if err != nil {
			return err
		}
		if m != "" {
			if err := s.Set("total_calls", m); err != nil {
				return err
			}
		}

		r, err := readLine(in, out, "Calls per second (calls_per_second / -r)", "1")
		if err != nil {
			return err
		}
		if r != "" {
			if err := s.Set("calls_per_second", r); err != nil {
				return err
			}
		}

		sp, err := readLine(in, out, "Periodic stats to stderr (-stat_period, e.g. 5s; empty=off)", "")
		if err != nil {
			return err
		}
		if sp != "" {
			if err := s.Set("stat_period", sp); err != nil {
				return err
			}
		}
	} else {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "UAS: listens for INVITE. Bind to the IP peers reach (not only 0.0.0.0 in Via).")

		ip, err := readLine(in, out, "Local bind IP (local_bind_ip / -i)", "127.0.0.1")
		if err != nil {
			return err
		}
		if ip == "" {
			fmt.Fprintln(errOut, "local IP is required for UAS in most setups")
			return fmt.Errorf("wizard: missing local_bind_ip")
		}
		if err := s.Set("local_bind_ip", ip); err != nil {
			return err
		}

		p, err := readLine(in, out, "Local UDP port (listen_port / -p)", "5060")
		if err != nil {
			return err
		}
		if p != "" {
			if err := s.Set("listen_port", p); err != nil {
				return err
			}
		}

		t, err := readLine(in, out, "Server transport (transport / -t): s1 or sn (UDP server)", "s1")
		if err != nil {
			return err
		}
		if t != "" {
			if err := s.Set("transport", t); err != nil {
				return err
			}
		}

		m, err := readLine(in, out, "Max calls to accept before exit (total_calls / -m)", "10000")
		if err != nil {
			return err
		}
		if m != "" {
			if err := s.Set("total_calls", m); err != nil {
				return err
			}
		}

		sp, err := readLine(in, out, "Periodic stats to stderr (-stat_period, e.g. 5s; empty=off)", "")
		if err != nil {
			return err
		}
		if sp != "" {
			if err := s.Set("stat_period", sp); err != nil {
				return err
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Wizard done. Review: show   suggestions: hint   then: run")
	return nil
}
