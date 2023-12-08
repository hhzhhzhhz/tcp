package tcp

import (
	"encoding/binary"
	"fmt"
	"github.com/google/gopacket/layers"
	"math/rand"
	"time"
)

// AnalysisLayers todo tcp 协议
func AnalysisLayers(tcp *layers.TCP) Flag {
	switch true {
	case tcp.PSH && tcp.ACK:
		return pshAck
	case tcp.SYN && tcp.ACK:
		return synAck
	case tcp.RST && tcp.ACK:
		return rstAck
	case tcp.RST:
		return rstAck
	case tcp.FIN && tcp.ACK:
		return fin
	case tcp.URG && tcp.ACK:
		return urg
	case tcp.SYN:
		return syn
	case tcp.ACK && len(tcp.Payload) == 1:
		return keepAlive
	case tcp.ACK:
		return dataAck
	}
	return unknown
}

func (f Flag) String() string {
	switch f {
	case pshAck:
		return "pshAck"
	case synAck:
		return "synAck"
	case rstAck:
		return "rstAck"
	case keepAlive:
		return "keepAlive"
	case dataAck:
		return "dataAck"
	case fin:
		return "fin"
	case urg:
		return "urg"
	case syn:
		return "syn"
	default:
		return "unknown"
	}
}

func FirstSeq() uint32 {
	seed := make([]byte, 8)
	_, err := rand.Read(seed)
	if err != nil {
		panic(err)
	}
	// 使用时间戳和随机数生成ISN
	isn := binary.BigEndian.Uint32(seed) ^ uint32(time.Now().UnixNano()/1000000)
	return isn
}

func PacketInfo(t *layers.TCP) string {
	return fmt.Sprintf("s_port=%d d_port=%d seq=%d ack=%d, len=%d win=%d df=%d fin=%t syn=%t rst=%t psh=%t ack=%t urg=%t ece=%t cwr=%t ns=%t",
		t.SrcPort, t.DstPort, t.Seq, t.Ack, len(t.BaseLayer.Payload), t.Window, t.DataOffset, t.FIN, t.SYN, t.RST, t.PSH, t.ACK, t.URG, t.ECE, t.CWR, t.NS)
}
