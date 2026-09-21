package siplog

import (
	"fmt"
	"net"
	"time"
)

const persistQueue = 8192

type persistKind int

const (
	jobSIP persistKind = iota
	jobApp
	jobRTP
	jobClear
	jobFlush
)

type persistJob struct {
	kind persistKind
	sum  CallSummary
	rec  Record
	rtp  RTPPacket
	done chan struct{}
}

func (c *Catalog) openPersist(path string) error {
	if c == nil {
		return fmt.Errorf("calls catalog is nil")
	}
	c.closePersist()
	st, err := openCallStore(path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.persist = st
	c.jobs = make(chan persistJob, persistQueue)
	c.workerDone = make(chan struct{})
	c.persistOn.Store(true)
	jobs := c.jobs
	done := c.workerDone
	c.mu.Unlock()
	go persistWorker(st, jobs, done)
	return nil
}

func (c *Catalog) closePersist() {
	if c == nil {
		return
	}
	c.persistOn.Store(false)
	c.Flush()
	c.mu.Lock()
	jobs := c.jobs
	done := c.workerDone
	st := c.persist
	c.jobs = nil
	c.workerDone = nil
	c.persist = nil
	c.mu.Unlock()
	if jobs != nil {
		close(jobs)
	}
	if done != nil {
		<-done
	}
	if st != nil {
		_ = st.Close()
	}
}

func (c *Catalog) enqueue(j persistJob) {
	if c == nil {
		return
	}
	if j.kind != jobFlush && j.kind != jobClear && !c.persistOn.Load() {
		return
	}
	c.mu.Lock()
	jobs := c.jobs
	c.mu.Unlock()
	if jobs == nil {
		return
	}
	if j.kind == jobFlush || j.kind == jobClear {
		jobs <- j
		return
	}
	select {
	case jobs <- j:
	default:
	}
}

// Flush waits until queued SQLite writes have been applied. No-op without persist.
func (c *Catalog) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	jobs := c.jobs
	c.mu.Unlock()
	if jobs == nil {
		return
	}
	done := make(chan struct{})
	select {
	case jobs <- persistJob{kind: jobFlush, done: done}:
	case <-time.After(5 * time.Second):
		return
	}
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
}

func persistWorker(st *callStore, jobs <-chan persistJob, done chan struct{}) {
	defer close(done)
	writes := 0
	for j := range jobs {
		batch := []persistJob{j}
		for len(batch) < 64 {
			select {
			case next, ok := <-jobs:
				if !ok {
					goto apply
				}
				batch = append(batch, next)
			default:
				goto apply
			}
		}
	apply:
		var flushes []chan struct{}
		for _, job := range batch {
			switch job.kind {
			case jobSIP, jobApp:
				_ = st.Upsert(job.sum)
				_ = st.InsertEvent(job.rec)
				writes++
			case jobRTP:
				_ = st.Upsert(job.sum)
				_ = st.InsertRTP(job.rtp)
				writes++
			case jobClear:
				_ = st.Clear()
				writes = 0
			case jobFlush:
				if job.done != nil {
					flushes = append(flushes, job.done)
				}
			}
		}
		if writes >= 16 {
			_ = st.Prune(persistCap)
			writes = 0
		}
		for _, ch := range flushes {
			close(ch)
		}
	}
}

func cloneRTP(p RTPPacket) RTPPacket {
	p.Payload = append([]byte(nil), p.Payload...)
	if p.SrcIP != nil {
		p.SrcIP = append(net.IP(nil), p.SrcIP...)
	}
	if p.DstIP != nil {
		p.DstIP = append(net.IP(nil), p.DstIP...)
	}
	return p
}
