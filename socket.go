package tcp

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/hhzhhzhhz/tcp/log"
	"net"
	"sync"
	"time"
)

// Status tcp 状态
type Status int

const (
	SynSend Status = iota
	SynRecv

	EstabLished

	FinWait1
	FinWait2
	TimeWait

	CloseWait
	LastAck

	Closed
)

type Flag int

const (
	synAck    Flag = iota
	keepAlive      // 链接维持包
	rstAck         // 链接重置
	pshAck         // 数据包
	dataAck        // 数据确认包
	window         // 窗口大小确认包
	syn            // 重传包
	fin            // 关闭数据包
	urg            // 紧急指针
	unknown        // 未知数据包
)

const (
	windowSize       = 4096
	defaultRetry     = 2
	MaxSendSize      = 1400 // 网卡发送数据限制1 MTU
	windowBufferSize = 8192
	keepAliveTime    = 5 * time.Second
)

var (
	protocol = "tcp"
)

var byteSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]byte, windowBufferSize)
	},
}

var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// DailTcpV todo 实现arp根据ip获取mac地址
func DailTcpV(srcMac, DstMac net.HardwareAddr, srcAddr, dstAddr string, dw DriveWrapper, timeout time.Duration) (net.Conn, error) {
	ld, err := net.ResolveTCPAddr(protocol, srcAddr)
	if err != nil {
		return nil, err
	}
	rd, err := net.ResolveTCPAddr(protocol, dstAddr)
	if err != nil {
		return nil, err
	}
	soc := NewSocket(srcMac, DstMac, ld, rd)
	if err = soc.dailTcp(dw, timeout); err != nil {
		return nil, err
	}
	return soc, nil
}

type socket struct {
	ctx              context.Context
	status           Status // tcp 状态
	cancel           context.CancelFunc
	closed           bool // 关闭
	mux              sync.Mutex
	once             sync.Once
	srcMac, dstMac   net.HardwareAddr // mac 地址
	srcIp, dstIp     net.IP           // 源ip 目标ip
	srcPort, dstPort layers.TCPPort   // 源端口 目标端口
	win              uint16           // tcp 窗口大小, 解析包使用了uint16, 因此最大值只有 65535
	Sseq             uint32           // 下一个接收端序列号
	SCurSeq          uint32           // 当前接收端序列号
	first            bool             //
	lastExchange     int64            // 最后一次与服务端交换数据时间, 用来设置keepalive 逻辑
	Cseq             uint32           // 发送端序列号
	rb               *Buffer
	wb               *Buffer
	writeOd          *TcpOrderly
	fd               *Fd
	stop             chan struct{}
	closeCallback    func()
}

func NewSocket(srcMac, dstMac net.HardwareAddr, srcAddr, dstAddr *net.TCPAddr) *socket {
	ctx, cancel := context.WithCancel(context.Background())
	return &socket{
		ctx:          ctx,
		cancel:       cancel,
		status:       Closed,
		closed:       false,
		srcMac:       srcMac,
		dstMac:       dstMac,
		srcIp:        srcAddr.IP,
		dstIp:        dstAddr.IP,
		win:          windowSize,
		rb:           NewBuffer(),
		wb:           NewBuffer(),
		writeOd:      NewTcpOrderly(),
		srcPort:      layers.TCPPort(uint16(srcAddr.Port)),
		first:        true,
		dstPort:      layers.TCPPort(uint16(dstAddr.Port)),
		lastExchange: time.Now().Unix(),
		stop:         make(chan struct{}, 2),
	}
}

func (s *socket) Read(b []byte) (int, error) {
	return s.rb.Read(b)
}

func (s *socket) Write(b []byte) (int, error) {
	return s.wb.Write(b)
}

func (s *socket) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: s.srcIp, Port: int(s.srcPort)}
}

func (s *socket) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: s.dstIp, Port: int(s.dstPort)}
}

// SetDeadline todo
func (s *socket) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline todo
func (s *socket) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline todo
func (s *socket) SetWriteDeadline(t time.Time) error {
	return nil
}

func (s *socket) src() string {
	return fmt.Sprintf("%s:%d", s.srcIp.String(), s.srcPort)
}

func (s *socket) dst() string {
	return fmt.Sprintf("%s:%d", s.dstIp.String(), s.dstPort)
}

// dailTcp 三次握手建立tcp连接
func (s *socket) dailTcp(dw DriveWrapper, timeout time.Duration) error {
	var err error
	s.fd, err = dw.CreateFd(*s.LocalAddr().(*net.TCPAddr), *s.RemoteAddr().(*net.TCPAddr))
	if err != nil {
		return err
	}
	s.Cseq = FirstSeq()
	if err = s.sendPacket(true, false, false, false, false, s.Cseq, 0); err != nil {
		return err
	}
	// tcp 状态变为syncSend
	s.status = SynSend
	log.Logger().Info("%s send syn to %s seq=%d ack=%d", s.src(), s.dst(), s.Cseq, 0)
	// next seq
	s.Cseq = s.Cseq + 1
	// wait SynAck -> SYN=1, ACK=1, seq=y, ack=x+1
	after := time.After(timeout)
	for {
		select {
		case <-after:
			return fmt.Errorf("dail tcp failed cause= read timeout %ds", int(timeout.Seconds()))
		case packet, ok := <-s.fd.Read():
			if !ok {
				return fmt.Errorf("%s -> %s fd closed dial failed", s.dst(), s.src())
			}
			s.writeOd.ack(packet.Ack)
			switch AnalysisLayers(packet) {
			case rstAck:
				// 链接重置, 释放fd
				dw.ReleaseFd(s.fd)
				return fmt.Errorf("%s recv rst packet from %s seq=%d ack=%d dail failed", s.src(), s.dst(), packet.Seq, packet.Ack)
			case synAck:
				// next seq
				s.Sseq = packet.Seq + 1
				s.SCurSeq = packet.Seq + 1
				s.win = packet.Window
				log.Logger().Info("%s received syn+ack from %s seq=%d ack=%d", s.src(), s.dst(), packet.Seq, packet.Ack)
				// 接收到第二次握手包, 返回ack
				if err = s.sendPacket(false, true, false, false, false, s.Cseq, s.Sseq); err != nil {
					return err
				}
				log.Logger().Info("%s send an ack response to %s seq=%d ack=%d", s.src(), s.dst(), s.Cseq, s.Sseq)
				goto receive
			default:
				return fmt.Errorf("receive unknown src=%s dst=%s  packet=%s seq=%d ack=%d", s.dst(), s.src(), PacketInfo(packet), packet.Seq, packet.ACK)
			}
		}
	}
receive:
	log.Logger().Info("begin handle %s to %s", s.src(), s.dst())
	s.closeCallback = func() { dw.ReleaseFd(s.fd) }
	s.status = EstabLished
	go s.fdRead()
	go s.fdWrite()
	return nil
}

// 读取服务端返回的数据发送给客户端
// 当出现数据连续, 会一直等待客户端重传
func (s *socket) fdRead() {
	for {
		select {
		case packet, ok := <-s.fd.Read():
			if !ok {
				for packet = range s.fd.Read() {
					s.handleTcp(packet)
				}
				log.Logger().Error("%s -> %s fd read closed", s.dst(), s.src())
				return
			}
			s.handleTcp(packet)
		}
	}
}

// 流量控制, 数据顺序
// 上层保证读取包大小在1024
// 读取客户端发送的数据发送给服务端
func (s *socket) fdWrite() {
	for {
		// 发送的最大数据长度
		buff := bufferPool.Get().(*bytes.Buffer)
		temp := byteSlicePool.Get().([]byte)
		n, err := s.wb.Read(temp)
		if err != nil {
			log.Logger().Error("%s -> %s fd write stop cause=chan closed len=%d", s.src(), s.dst(), s.wb.Len())
			return
		}
		buff.Write(temp[:n])
		temp = temp[:cap(temp)]
		byteSlicePool.Put(temp)
		size := buff.Len()
		if size == 0 {
			continue
		}
		buff.Bytes()
		s.slidingWindow(buff.Bytes())
		buff.Reset()
		bufferPool.Put(buff)
	}
}

func (s *socket) min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 分段发送, 每个数据包不能大于一个MTU
func (s *socket) slidingWindow(data []byte) {
	size := len(data)
	maxSize := MaxSendSize
	seq := s.Cseq
	for {
		// todo
		// ack := s.Sseq
		ack := s.Sseq
		for p := 0; p < size; p += maxSize {
			nd := data[p:s.min(p+maxSize, size)]
			s.writePacket(nd, seq, ack)
			seq = seq + uint32(len(nd))
		}
		// 保证tcp 数据有序发送
		dAck := s.Cseq + uint32(len(data))
		select {
		case <-s.writeOd.slippage(dAck, NewWait(dAck)):
			log.Logger().Debug("%s send data to %s seq=%d ack=%d len=%d wait_ack=%d", s.src(), s.dst(), seq, ack, len(data), dAck)
			goto seqAdd
		case <-time.After(time.Second * 5):
			log.Logger().Error("%s send data to %s seq=%d ack=%d len=%d wait_ack=%d timeout", s.src(), s.dst(), seq, ack, len(data), dAck)
			seq = s.Cseq
			continue
		case <-s.ctx.Done():
			log.Logger().Error("%s send data to %s seq=%d ack=%d len=%d wait_ack=%d ctx cancel", s.src(), s.dst(), seq, ack, len(data), dAck)
			return
		}
	}
seqAdd:
	// next seq
	s.Cseq = s.Cseq + uint32(size)

}

func (s *socket) writePacket(data []byte, seq, ack uint32) {
	retry := 0
	for {
		var err error
		var stream []byte

		if stream, err = s.packet(false, true, false, false, true, seq, ack, data...); err != nil {
			log.Logger().Error("%s send data to %s seq=%d ack=%d len=%d data serializeLayers failed cause=%s", s.src(), s.dst(), seq, ack, len(data), err.Error())
			break
		}
		// 写入网卡如果发生错误, 请检查是否数据超过单个MTU 限制
		if _, err = s.fd.Write(stream); err != nil && retry < defaultRetry {
			retry++
			log.Logger().Error("%s send failed to %s seq=%d ack=%d len=%d cause=%s", s.src(), s.dst(), seq, ack, len(data), err.Error())
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}
}

func (s *socket) handleTcp(packet *layers.TCP) {
	var err error
	// 记录服务端的ack确认包
	s.writeOd.ack(packet.Ack)
	s.lastExchange = time.Now().Unix()
	switch AnalysisLayers(packet) {
	case rstAck:
		log.Logger().Warn("%s receive rst from %s seq=%d ack=%d", s.src(), s.dst(), packet.Seq, packet.Ack)
		if packet.Window == 2 {
			return
		}
		if !s.closed {
			go s.close()
		}
		// 断开数据包, 握手失败, 释放
		s.closed = true
		log.Logger().Warn("%s -> %s socket closed", s.src(), s.dst())
	case fin:
		log.Logger().Debug("%s receive fin from %s seq=%d ack=%d", s.src(), s.dst(), packet.Seq, packet.Ack)
	case keepAlive:
		// 已经处于关闭
		if s.status > EstabLished {
			return
		}
		// next seq
		if !s.first && packet.Seq > s.Sseq {
			s.Sseq = packet.Seq
			s.SCurSeq = packet.Seq
		}
		if err = s.sendPacket(false, true, false, false, false, s.Cseq, s.Sseq); err != nil {
			log.Logger().Warn("%s send keepalive ack to %s failed cause=%s", s.src(), s.dst(), err.Error())
			return
		}
		log.Logger().Debug("%s send keepalive seq=%d ack=%d to %s", s.src(), s.Cseq, s.Sseq, s.dst())
	case pshAck, dataAck:
		if packet.PSH == false && len(packet.Payload) == 0 {
			if !s.first && packet.Seq > s.Sseq {
				//s.Sseq = packet.Seq
				s.SCurSeq = packet.Seq
			}
			return
		}
		// 判断是否处于关闭
		if s.status > EstabLished {
			return
		}
		// 保证数据有序行
		if s.Sseq != packet.Seq {
			log.Logger().Debug("%s receive data from %s len=%d seq=%d but wrong seq number wait_seq=%d", s.src(), s.dst(), len(packet.BaseLayer.Payload), packet.Seq, s.Sseq)
			if err = s.sendPacket(false, true, false, false, false, s.Cseq, s.Sseq); err != nil {
				log.Logger().Error("%s send data ack seq=%d ack=%d to %s failed cause=%s", s.src(), s.dst(), s.Cseq, s.Sseq, err.Error())
			}
			return
		}
		log.Logger().Debug("%s receive data from %s len=%d seq=%d ack=%d", s.src(), s.dst(), len(packet.BaseLayer.Payload), packet.Seq, packet.Ack)
		if s.first {
			s.first = false
		}
		s.Sseq = packet.Seq + uint32(len(packet.Payload))
		s.SCurSeq = packet.Seq
		// 数据包
		if err = s.sendPacket(false, true, false, false, false, s.Cseq, s.Sseq); err != nil {
			log.Logger().Error("%s send data ack seq=%d ack=%d to %s failed cause=%s", s.src(), s.dst(), s.Cseq, s.Sseq, err.Error())
			return
		}
		log.Logger().Debug("%s send data ack seq=%d ack=%d to %s", s.src(), s.Cseq, s.Sseq, s.dst())
		s.rb.Write(packet.Payload)

	case window:
		if !s.first && packet.Seq > s.Sseq {
			s.Sseq = packet.Seq
			s.SCurSeq = packet.Seq
		}
		s.win = packet.Window
	default:
		if !s.first && packet.Seq > s.Sseq {
			s.Sseq = packet.Seq
			s.SCurSeq = packet.Seq
		}
		log.Logger().Warn("%s receive unknown packet from %s data=%s", s.src(), s.dst(), PacketInfo(packet))
	}
}

func (s *socket) eth() []gopacket.SerializableLayer {
	var sls []gopacket.SerializableLayer
	physical := &layers.Ethernet{EthernetType: layers.EthernetTypeIPv4, SrcMAC: s.srcMac, DstMAC: s.dstMac}
	sls = append(sls, physical)
	return sls
}

func (s *socket) ip() *layers.IPv4 {
	return &layers.IPv4{Version: 4, IHL: 5, TOS: 0, Flags: layers.IPv4DontFragment, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: s.srcIp, DstIP: s.dstIp}
}

// 构建tcp 数据包 发送
func (s *socket) sendPacket(syn, ack, fin, rst, psh bool, seqNum, ackNum uint32, data ...byte) error {
	stream, err := s.packet(syn, ack, fin, rst, psh, seqNum, ackNum, data...)
	if err != nil {
		return err
	}
	_, err = s.fd.Write(stream)
	return err
}

// 构建数据包
func (s *socket) packet(syn, ack, fin, rst, psh bool, seqNum, ackNum uint32, data ...byte) (stream []byte, err error) {
	// 物理层数据
	sls := s.eth()
	// ip层数据
	ipV4 := s.ip()
	sls = append(sls, ipV4)
	// tcp层数据
	tcp := &layers.TCP{
		SrcPort: layers.TCPPort(s.srcPort),
		DstPort: layers.TCPPort(s.dstPort),
		Seq:     seqNum,
		Ack:     ackNum,
		SYN:     syn,
		ACK:     ack,
		FIN:     fin,
		RST:     rst,
		PSH:     psh,
		Window:  s.win,
	}
	tcp.SetNetworkLayerForChecksum(ipV4)
	sls = append(sls, tcp)
	if len(data) > 0 {
		sls = append(sls, gopacket.Payload(data))
	}
	buffer := gopacket.NewSerializeBuffer()
	if err = gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{ComputeChecksums: true, FixLengths: true}, sls...); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Close 暴力关闭socket 发送rst 释放链接, 会等待对端也发送rst
func (s *socket) Close() error {
	if s.closed {
		select {
		case <-s.stop:
		case <-time.After(1 * time.Minute):
		}
		return nil
	}
	retry := 0
	for {
		seq := s.Cseq
		ack := s.Sseq
		var err error
		if err = s.sendPacket(false, false, true, false, false, seq, ack); err != nil {
			log.Logger().Error("%s send rst seq=%d ack=%d to %s failed cause=%s", s.src(), seq, ack, s.dst(), err.Error())
			time.Sleep(2 * time.Second)
			continue
		}
		log.Logger().Info("%s send rst seq=%d ack=%d to %s", s.src(), seq, ack, s.dst())
		retry++
		time.Sleep(2 * time.Second)
		if !s.closed && retry < 1 {
			continue
		}
		break
	}
	log.Logger().Warn("%s -> %s socket closed", s.src(), s.dst())
	return s.close()
}

func (s *socket) close() error {
	s.once.Do(func() {
		// 10s 后释放资源
		time.Sleep(30 * time.Second)
		s.cancel()
		s.wb.Close()
		time.Sleep(10 * time.Second)
		s.rb.Close()
		if s.closeCallback != nil {
			s.closeCallback()
		}
		s.stop <- struct{}{}
	})
	return nil
}

// 四次挥手关闭 todo
func (s *socket) releaseTcp() error {
	timeout := 10 * time.Second
	after := time.After(timeout)
	s.Cseq = s.Cseq + 1
	log.Logger().Info("%s send fin packet to %s seq=%d ack=%d", s.LocalAddr().String(), s.RemoteAddr().String(), s.Cseq, s.Sseq)
	if err := s.sendPacket(false, true, true, false, false, s.Cseq, s.Sseq); err != nil {
		return err
	}
	s.status = FinWait1
	for {
		select {
		case <-after:
			return fmt.Errorf("四次挥手超时 %ds", int(timeout.Seconds()))
		case <-s.writeOd.slippage(s.Cseq+1, NewWait(s.Cseq+1)):
			s.status = FinWait2
			log.Logger().Info("%s rev ack from %s seq=%d ack=%d", s.LocalAddr().String(), s.RemoteAddr().String(), s.Sseq, s.Sseq)
		}
	}
}
