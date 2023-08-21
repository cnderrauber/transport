package udp

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"
)

// const (
// 	writeSpeedUpdateInterval = time.Second
// 	targetWriteInterval      = 2 * time.Millisecond
// )

type batchWriter interface {
	WriteBatch(ms []ipv4.Message, flags int) (int, error)
}

type batchReader interface {
	ReadBatch(msg []ipv4.Message, flags int) (int, error)
}

type batchPacketConn interface {
	batchWriter
	batchReader
}

type BatchConn struct {
	net.PacketConn

	batchConn batchPacketConn

	batchWriteMutex    sync.Mutex
	batchWriteMessages []ipv4.Message
	batchWritePos      int
	batchWriteLast     time.Time

	// readBatchSize      int
	// todo : variable batch write size based on past average write speed (pps)
	batchWriteSize     int
	batchWriteInterval time.Duration

	closed atomic.Bool

	// adaptive batch size
	// writeCounter         int
	// lastWriteCounterTime time.Time
	// currentPPS           int
}

func NewBatchConn(conn net.PacketConn, batchWriteSize int, batchWriteInterval time.Duration) *BatchConn {
	bc := &BatchConn{
		PacketConn:         conn,
		batchWriteLast:     time.Now(),
		batchWriteInterval: batchWriteInterval,
		batchWriteSize:     batchWriteSize,
		batchWriteMessages: make([]ipv4.Message, batchWriteSize),
	}
	for i := range bc.batchWriteMessages {
		bc.batchWriteMessages[i].Buffers = [][]byte{make([]byte, sendMTU)}
	}

	// batch write only supports linux
	if runtime.GOOS == "linux" {
		if pc4 := ipv4.NewPacketConn(conn); pc4 != nil {
			bc.batchConn = pc4
		} else if pc6 := ipv4.NewPacketConn(conn); pc6 != nil {
			bc.batchConn = pc6
		}
	}

	if bc.batchConn != nil {
		go func() {
			writeTicker := time.NewTicker(batchWriteInterval / 2)
			defer writeTicker.Stop()

			// speedUpdateTicker := time.NewTicker(writeSpeedUpdateInterval)
			// defer speedUpdateTicker.Stop()

			for !bc.closed.Load() {
				select {
				case <-writeTicker.C:
					bc.batchWriteMutex.Lock()
					if bc.batchWritePos > 0 && time.Since(bc.batchWriteLast) >= bc.batchWriteInterval {
						bc.flush()
					}
					bc.batchWriteMutex.Unlock()

					// case <-speedUpdateTicker.C:
					// 	bc.updateWriteSpeed()
				}
			}
		}()
	}

	return bc
}

func (c *BatchConn) Close() error {
	c.closed.Store(true)
	return c.PacketConn.Close()
}

func (c *BatchConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.batchConn == nil {
		return c.PacketConn.WriteTo(b, addr)
	}
	return c.writeBatch(b, addr)
}

func (c *BatchConn) writeBatch(buf []byte, raddr net.Addr) (int, error) {
	var err error
	c.batchWriteMutex.Lock()
	defer c.batchWriteMutex.Unlock()

	// c.writeCounter++
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
	if c.batchWritePos == c.batchWriteSize {
		err = c.flush()
	}
	return len(buf), err
}

func (c *BatchConn) flush() error {
	var writeErr error
	var txN int
	for txN < c.batchWritePos {
		if n, err := c.batchConn.WriteBatch(c.batchWriteMessages[txN:c.batchWritePos], 0); err != nil {
			writeErr = err
			break
		} else {
			txN += n
		}
	}
	c.batchWritePos = 0
	c.batchWriteLast = time.Now()
	return writeErr
}

// func (c *BatchConn) updateWriteSpeed() {
// 	c.batchWriteMutex.Lock()
// 	defer c.batchWriteMutex.Unlock()

// 	diff := time.Since(c.lastWriteCounterTime)

// 	pps := c.writeCounter * 1000 / int(diff.Milliseconds())
// 	c.writeCounter = 0
// 	c.lastWriteCounterTime = time.Now()

// 	switch {
// 	case c.currentPPS == 0:
// 		c.currentPPS = pps
// 	case pps > c.currentPPS:
// 		c.currentPPS = c.currentPPS + (pps-c.currentPPS)/3
// 	case pps < c.currentPPS:
// 		c.currentPPS = c.currentPPS + (pps-c.currentPPS)/2
// 	}

// 	writeInterval := targetWriteInterval
// 	if writeInterval< writeInterval {

// 	// TODO: adjust batch size based on currentPPS
// 	if c.currentPPS > c.batchWriteSize {
// 	}

// }

func (c *BatchConn) ReadBatch(msgs []ipv4.Message, flags int) (int, error) {
	if c.batchConn == nil {
		n, addr, err := c.PacketConn.ReadFrom(msgs[0].Buffers[0])
		if err == nil {
			msgs[0].N = n
			msgs[0].Addr = addr
			return 1, nil
		}
		return 0, err
	}
	return c.batchConn.ReadBatch(msgs, flags)
}
