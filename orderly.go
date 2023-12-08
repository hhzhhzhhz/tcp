package tcp

import (
	"sync"
	"time"
)

type wait struct {
	w        chan struct{}
	createAt int64
	seq      uint32
	//len  uint32
}

func NewWait(seq uint32) *wait {
	return &wait{w: make(chan struct{}, 5), seq: seq, createAt: time.Now().Unix()}
}

// NewTcpOrderly 此模块保证tcp 发送顺序
func NewTcpOrderly() *TcpOrderly {
	t := &TcpOrderly{hacks: make(map[uint32]*wait, 512)}
	//go t.clearing()
	return t
}

// TcpOrderly 该模块保证tcp数据有序
type TcpOrderly struct {
	mux   sync.Mutex
	hacks map[uint32]*wait
}

// seqnum + len(data)
func (t *TcpOrderly) slippage(seq uint32, w *wait) <-chan struct{} {
	t.mux.Lock()
	defer t.mux.Unlock()
	t.hacks[seq] = w
	return w.w
}

// ack
func (t *TcpOrderly) ack(ack uint32) {
	t.mux.Lock()
	defer t.mux.Unlock()
	aw, ok := t.hacks[ack]
	if ok {
		select {
		case aw.w <- struct{}{}:
		default:
		}
		delete(t.hacks, ack)
	}
}
