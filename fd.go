package tcp

import (
	"fmt"
	"github.com/google/gopacket/layers"
	"go.uber.org/atomic"
	"io"
	"net"
	"time"
)

type Fd struct {
	close    *atomic.Bool
	pipe     chan *layers.TCP
	device   io.Writer
	src      net.TCPAddr
	dst      net.TCPAddr
	createAt int64
	updateAt int64
}

func NewFd(src net.TCPAddr, dst net.TCPAddr, device io.Writer) *Fd {
	n := time.Now().Unix()
	fd := &Fd{
		close:    atomic.NewBool(false),
		device:   device,
		src:      src,
		dst:      dst,
		pipe:     make(chan *layers.TCP, 2048),
		createAt: n,
		updateAt: n,
	}
	return fd
}

func (l *Fd) WriteFd(tcp *layers.TCP) error {
	if l.IsClose() {
		return fmt.Errorf("fd closed")
	}
	l.updateAt = time.Now().Unix()
	l.pipe <- tcp
	return nil
}

func (l *Fd) Read() <-chan *layers.TCP {
	return l.pipe
}

//func (l *Fd) Hash() string {
//	return fmt.Sprintf("src_%s->dst_%s", l.src.String(), l.dst.String())
//}

func (l *Fd) Write(data []byte) (n int, err error) {
	return l.device.Write(data)
}

func (l *Fd) IsClose() bool {
	return l.close.Load()
}

func (l *Fd) Close() {
	if l.close.Load() {
		return
	}
	l.close.Store(true)
	close(l.pipe)
}
