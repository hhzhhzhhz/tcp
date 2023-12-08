package tcp

import (
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/hhzhhzhhz/tcp/log"
	"net"
)

type Layer int

const (
	Ethernet Layer = 2
	Tcp      Layer = 3
)

type Drive interface {
	Packets() chan gopacket.Packet
	Write(data []byte) (n int, err error)
	Close() error
	Addr() string
	Layer() Layer
}

type DriveWrapper interface {
	CreateFd(src net.TCPAddr, dst net.TCPAddr) (*Fd, error)
	ReleaseFd(fd *Fd)
	// Mac() net.HardwareAddr
}

// todo 实现驱动接口
type tunDr struct {
	addr string
}

func (td *tunDr) Addr() string {
	return td.addr
}

func (td *tunDr) Layer() Layer {
	return Tcp
}

func (td *tunDr) Packets() chan gopacket.Packet {
	return nil
}

func (td *tunDr) Write(data []byte) (n int, err error) {
	return 0, nil
}

func (td *tunDr) Close() error {
	return nil
}

type Option struct {
	// Ports []int
}

type driveWrapper struct {
	option *Option
	drive  Drive
	fds    map[string]*Fd
	mac    net.HardwareAddr
}

func NewDriveWrapper(drive Drive, ops *Option) (DriveWrapper, error) {
	ps := &driveWrapper{drive: drive, option: ops, fds: make(map[string]*Fd, 0)}
	go ps.polling()
	return ps, nil
}

func (ps *driveWrapper) CreateFd(src net.TCPAddr, dst net.TCPAddr) (*Fd, error) {
	hash := ps.hash(src, dst)
	fd := ps.fds[hash]
	if fd != nil {
		return nil, fmt.Errorf("src=%s dst=%s port already in use", src.String(), dst.String())
	}
	fd = NewFd(src, dst, ps.drive)
	ps.fds[hash] = fd
	return fd, nil
}

func (ps *driveWrapper) ReleaseFd(fd *Fd) {
	fd.Close()
	ps.fds[ps.hash(fd.src, fd.dst)] = nil
}

func (ps *driveWrapper) needForward(dstPort uint16) bool {
	//for _, port := range ps.option.Ports {
	//	if uint16(port) == dstPort {
	//		return true
	//	}
	//}
	return true
}

func (ps *driveWrapper) polling() {
	for {
		select {
		case packet, ok := <-ps.drive.Packets():
			if !ok {
				log.Logger().Error("nic is closed")
				return
			}
			ps.handle(packet)
		}
	}
}

func (ps *driveWrapper) hash(src net.TCPAddr, dst net.TCPAddr) string {
	return fmt.Sprintf("src_%s->dst_%s", src.String(), dst.String())
}

func (ps *driveWrapper) handle(packet gopacket.Packet) {
	ipLy := packet.Layer(layers.LayerTypeIPv4)
	if ipLy == nil {
		//log.Logger().Error("ip layer is empty")
		return
	}
	tcpLy := packet.Layer(layers.LayerTypeTCP)
	if tcpLy == nil {
		//log.Logger().Error("tcp layer is empty")
		return
	}
	var src, dst net.TCPAddr
	ip, ok := ipLy.(*layers.IPv4)
	if !ok {
		log.Logger().Error("invalid ipv4")
		return
	}
	srcIp := ip.SrcIP
	dstIp := ip.DstIP
	tcp, ok := tcpLy.(*layers.TCP)
	if !ok {
		log.Logger().Error("invalid tcp")
		return
	}
	srcPort := tcp.SrcPort
	dstPort := tcp.DstPort
	src = net.TCPAddr{IP: srcIp, Port: int(srcPort)}
	dst = net.TCPAddr{IP: dstIp, Port: int(dstPort)}
	hash := ps.hash(dst, src)
	fd, ok := ps.fds[hash]
	if !ok {
		return
	}
	if err := fd.WriteFd(tcp); err != nil {
		log.Logger().Error("%s write fd failed cause=%s", PacketInfo(tcp), err.Error())
	}

}

func (ps *driveWrapper) Close() {
	ps.drive.Close()
}
