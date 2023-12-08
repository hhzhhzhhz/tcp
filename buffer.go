package tcp

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	waitTime = 10 * time.Millisecond
)

type Buffer struct {
	mux    sync.RWMutex
	closed bool
	buf    *bytes.Buffer
}

func NewBuffer() *Buffer {
	return &Buffer{
		closed: false,
		buf:    &bytes.Buffer{},
	}
}

func (c *Buffer) Write(p []byte) (n int, err error) {
	if c.closed {
		return 0, fmt.Errorf("buffer is closed")
	}
	c.mux.Lock()
	n, err = c.buf.Write(p)
	c.mux.Unlock()
	return n, err
}

func (c *Buffer) Read(p []byte) (n int, err error) {
	for {
		// 没有数据且关闭
		if c.closed {
			//break
			return 0, fmt.Errorf("buffer is closed")
		}
		c.mux.RLock()
		n, err = c.buf.Read(p)
		if err == io.EOF {
			c.mux.RUnlock()
			time.Sleep(waitTime)
			continue
		}
		c.mux.RUnlock()
		break
	}
	return n, err
}

func (c *Buffer) Len() int {
	return c.buf.Len()
}

func (c *Buffer) Close() {
	c.mux.Lock()
	defer c.mux.Unlock()
	c.closed = true
}
