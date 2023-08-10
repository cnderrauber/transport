package udp

import (
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

type BatchConn struct {
	*net.UDPConn
	batchConn          *ipv4.PacketConn
	batchWriteMutex    sync.Mutex
	batchWriteMessages []ipv4.Message
	batchWritePos      int
	batchWriteLast     time.Time

	// readBatchSize      int
	batchWriteSize     int
	batchWriteInterval time.Duration
}

func NewBatchConn(conn *net.UDPConn, batchWriteSize int, batchWriteInterval time.Duration, delayBatch bool) *BatchConn {
	bc := &BatchConn{
		UDPConn:            conn,
		batchWriteLast:     time.Now(),
		batchWriteInterval: batchWriteInterval,
		batchWriteSize:     batchWriteSize,
		batchWriteMessages: make([]ipv4.Message, batchWriteSize),
	}
	for i := range bc.batchWriteMessages {
		bc.batchWriteMessages[i].Buffers = [][]byte{make([]byte, sendMTU)}
	}

	if  delayBatch {
		time.AfterFunc(10*time.Second, func() {
			bc.batchConn = ipv4.NewPacketConn(bc.UDPConn)
		})
	} else {
		bc.batchConn = ipv4.NewPacketConn(bc.UDPConn)
	}

	return bc
}

func (c *BatchConn) Write(b []byte) (int, error) {
	if c.batchConn == nil {
		return c.UDPConn.Write(b)
	}
	return c.writeBatch(b, nil)
}

func (c *BatchConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.batchConn == nil {
		return c.UDPConn.WriteTo(b, addr)
	}
	return c.writeBatch(b, addr)
}

func (c *BatchConn) writeBatch(buf []byte, raddr net.Addr) (int, error) {
	c.batchWriteMutex.Lock()
	defer c.batchWriteMutex.Unlock()

	msg := &c.batchWriteMessages[c.batchWritePos]
	// reset buffers
	msg.Buffers = msg.Buffers[:1]
	msg.Buffers[0] = msg.Buffers[0][:cap(msg.Buffers[0])]

	c.batchWritePos++
	if raddr != nil {
		msg.Addr = raddr
	}
	if n := copy(msg.Buffers[0], buf); n < len(buf) {
		// todo: use extra buffer to copy remaining bytes
	} else {
		msg.Buffers[0] = msg.Buffers[0][:n]
	}

	if c.batchWritePos == c.batchWriteSize || time.Since(c.batchWriteLast) > c.batchWriteInterval {
		var txN int
		for txN < c.batchWritePos {
			if n, err := c.batchConn.WriteBatch(c.batchWriteMessages[txN:c.batchWritePos], 0); err != nil {
				fmt.Println("write error", err, "msgs", c.batchWriteMessages[txN:c.batchWritePos])
				break
			} else {
				fmt.Print("write succc", n, " txN", txN, " batchWritePos", c.batchWritePos)
				txN += n
			}
		}
		c.batchWritePos = 0
	}
	return len(buf), nil
}
