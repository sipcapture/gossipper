package engine

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type commandNetwork struct {
	name     string
	peers    map[string]string
	listener net.Listener
	wg       sync.WaitGroup
}

func (e *Engine) startCommandNetwork(ctx context.Context) error {
	if e.cfg.CommandName == "" || len(e.cfg.CommandPeers) == 0 {
		return nil
	}
	addr, ok := e.cfg.CommandPeers[e.cfg.CommandName]
	if !ok {
		return fmt.Errorf("missing command address for instance %q", e.cfg.CommandName)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	e.cmdNet = &commandNetwork{
		name:     e.cfg.CommandName,
		peers:    e.cfg.CommandPeers,
		listener: listener,
	}
	e.cmdNet.wg.Add(1)
	go func() {
		defer e.cmdNet.wg.Done()
		go func() {
			<-ctx.Done()
			_ = listener.Close()
		}()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			e.cmdNet.wg.Add(1)
			go func(conn net.Conn) {
				defer e.cmdNet.wg.Done()
				defer conn.Close()
				for {
					raw, err := readCommandFrame(conn)
					if err != nil {
						if err != io.EOF && e.cfg.TraceMessages {
							fmt.Fprintf(os.Stdout, "cmd transport read error: %v\n", err)
						}
						return
					}
					callID := commandCallID(raw, "")
					if callID == "" {
						continue
					}
					e.commands.enqueue(callID, "", commandMessage{
						raw:  raw,
						from: commandSender(raw, conn.RemoteAddr().String()),
					})
				}
			}(conn)
		}
	}()
	return nil
}

func (e *Engine) stopCommandNetwork() {
	if e.cmdNet == nil {
		return
	}
	_ = e.cmdNet.listener.Close()
	e.cmdNet.wg.Wait()
	e.cmdNet = nil
}

func (e *Engine) sendCommand(dest, raw string) error {
	from := commandSender(raw, fallbackCommandSender(e.cfg.CommandName, dest))
	callID := commandCallID(raw, "")
	if callID == "" {
		return fmt.Errorf("command message must include Call-ID")
	}
	if e.cmdNet != nil && dest != "" && dest != e.cfg.CommandName {
		addr, ok := e.cmdNet.peers[dest]
		if !ok {
			return fmt.Errorf("unknown command peer %q", dest)
		}
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return err
		}
		defer conn.Close()
		if err := writeCommandFrame(conn, raw); err != nil {
			return err
		}
		return nil
	}
	e.commands.enqueue(callID, "", commandMessage{raw: raw, from: from})
	return nil
}

func writeCommandFrame(w io.Writer, raw string) error {
	data := []byte(raw)
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(data)))
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func readCommandFrame(r io.Reader) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", err
	}
	size := binary.BigEndian.Uint32(header)
	if size == 0 {
		return "", nil
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return "", err
	}
	return string(data), nil
}

func fallbackCommandSender(name, dest string) string {
	if name != "" {
		return name
	}
	return dest
}
