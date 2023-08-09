package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/pion/transport/v2/udp"
	"golang.org/x/net/ipv4"
)

var (
	listenPort    = flag.Int("l", 0, "listen port")
	batch         = flag.Bool("b", true, "batch mode")
	batchSize     = flag.Int("bs", 256, "batch size")
	batchInterval = flag.Duration("bi", 5*time.Millisecond, "batch interval")
	connectHost   = flag.String("c", "localhost:9091", "connect host")
	duration      = flag.Duration("d", 1*time.Minute, "duration")

	elapsedMs int64
	packet    int64
	bytes     int64

	totalPacket int64
	totalBytes  int64
)

func main() {
	flag.Parse()

	if *listenPort > 0 {
		server()
	} else {
		client()
	}
}

func server() {
	lc := udp.ListenConfig{
		Batch: udp.BatchIOConfig{
			Enable:             *batch,
			ReadBatchSize:      *batchSize,
			WriteBatchSize:     *batchSize,
			WriteBatchInterval: *batchInterval,
		},
	}

	laddr := net.UDPAddr{Port: *listenPort}
	listener, err := lc.Listen("udp", &laddr)
	if err != nil {
		panic(err)
	}

	// time.AfterFunc(*duration, func() {
	// 	listener.Close()
	// })

	go report()

	buf := make([]byte, 1400)
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("accept error:", err)
			break
		}
		fmt.Println("connected, raddr: ", conn, "err", err)
		go func(conn net.Conn) {
			defer conn.Close()
			for {
				n, err := conn.Read(buf)
				if err != nil {
					break
				}

				atomic.AddInt64(&packet, 1)
				atomic.AddInt64(&bytes, int64(n))

				atomic.AddInt64(&totalPacket, 1)
				atomic.AddInt64(&totalBytes, int64(n))
			}
		}(conn)
	}
}

func client() {
	raddr, err := net.ResolveUDPAddr("udp", *connectHost)
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		panic(err)
	}

	time.AfterFunc(*duration, func() {
		conn.Close()
	})
	go report()
	if *batch {
		pktConn := ipv4.NewPacketConn(conn)
		msgs := make([]ipv4.Message, *batchSize)
		for i := 0; i < *batchSize; i++ {
			msgs[i].Buffers = [][]byte{make([]byte, 1400)}
		}

		for {
			n, err := pktConn.WriteBatch(msgs, 0)
			if err != nil {
				if err == io.ErrClosedPipe {
					break
				}
				panic(err)
			}
			atomic.AddInt64(&packet, int64(n))
			atomic.AddInt64(&bytes, int64(n*1400))

			atomic.AddInt64(&totalPacket, int64(n))
			atomic.AddInt64(&totalBytes, int64(n*1400))
		}
	} else {
		buf := make([]byte, 1400)
		for {
			n, err := conn.Write(buf)
			if err != nil {
				if err == io.ErrClosedPipe {
					break
				}
				panic(err)
			}

			atomic.AddInt64(&packet, 1)
			atomic.AddInt64(&bytes, int64(n))

			atomic.AddInt64(&totalPacket, 1)
			atomic.AddInt64(&totalBytes, int64(n))
		}
	}
}

func report() {
	start := time.Now()
	lastReport := start
	tk := time.NewTicker(5 * time.Second)
	for {
		<-tk.C
		lastElapsed := time.Since(lastReport)
		kpps := float64(packet) / lastElapsed.Seconds() / 1000
		mbps := float64(bytes) * 8 / lastElapsed.Seconds() / 1e6
		lastReport = time.Now()
		atomic.StoreInt64(&packet, 0)
		atomic.StoreInt64(&bytes, 0)

		elapsed := time.Since(start)
		totalkPps := float64(totalPacket) / elapsed.Seconds() / 1000
		totalMbps := float64(totalBytes) * 8 / elapsed.Seconds() / 1e6
		fmt.Printf("elapsed: %d s, kpps: %.2f, mbps: %.2f, total kpps: %.2f, total mbps: %.2f \n", int(elapsed.Seconds()), kpps, mbps, totalkPps, totalMbps)
	}
}
