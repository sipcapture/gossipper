package media

import (
	"net"
	"sync"
)

var scaleBatchWriters sync.Map // *net.UDPConn -> scaleBatchWriter

func batchWriterForConn(conn *net.UDPConn) scaleBatchWriter {
	if v, ok := scaleBatchWriters.Load(conn); ok {
		return v.(scaleBatchWriter)
	}
	w := newScaleBatchWriter(conn)
	actual, _ := scaleBatchWriters.LoadOrStore(conn, w)
	return actual.(scaleBatchWriter)
}

func forgetBatchWriter(conn *net.UDPConn) {
	scaleBatchWriters.Delete(conn)
}
