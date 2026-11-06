package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// rtpPayload strips the RTP header (RFC 3550) and returns the payload.
func rtpPayload(b []byte) ([]byte, error) {
	if len(b) < 12 {
		return nil, errors.New("rtp: short packet")
	}
	if b[0]>>6 != 2 {
		return nil, errors.New("rtp: version")
	}
	padding := b[0]&0x20 != 0
	ext := b[0]&0x10 != 0
	cc := int(b[0] & 0x0f)
	off := 12 + 4*cc
	if len(b) < off {
		return nil, errors.New("rtp: short csrc")
	}
	if ext {
		if len(b) < off+4 {
			return nil, errors.New("rtp: short extension")
		}
		off += 4 + 4*int(binary.BigEndian.Uint16(b[off+2:off+4]))
		if len(b) < off {
			return nil, errors.New("rtp: short extension")
		}
	}
	end := len(b)
	if padding {
		pad := int(b[end-1])
		if pad == 0 || end-pad < off {
			return nil, errors.New("rtp: padding")
		}
		end -= pad
	}
	return b[off:end], nil
}

// audioPipelineArgs: PulseAudio null sink monitor → Opus (48 kHz,
// stereo, 20 ms) → RTP → UDP on localhost.
func audioPipelineArgs(port int) []string {
	line := "pulsesrc device=titan.monitor buffer-time=40000 latency-time=10000 do-timestamp=true" +
		" ! audioconvert ! audioresample ! audio/x-raw,rate=48000,channels=2" +
		" ! opusenc bitrate=96000 frame-size=20 audio-type=generic" +
		fmt.Sprintf(" ! rtpopuspay ! udpsink host=127.0.0.1 port=%d sync=false async=false", port)
	return append([]string{"/usr/bin/gst-launch-1.0", "-q"}, strings.Fields(line)...)
}

// audioStream runs the audio pipeline and delivers Opus packets.
type audioStream struct {
	p    *proc
	conn *net.UDPConn
	once sync.Once
	done chan struct{}
}

func startAudio(onPacket func([]byte)) (*audioStream, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	_ = conn.SetReadBuffer(1 << 20)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	p, err := startProc("gst-audio", audioPipelineArgs(port), baseEnv(), nil, true)
	if err != nil {
		conn.Close()
		return nil, err
	}
	a := &audioStream{p: p, conn: conn, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		buf := make([]byte, 65536)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !from.IP.IsLoopback() {
				continue
			}
			payload, err := rtpPayload(buf[:n])
			if err != nil || len(payload) == 0 {
				continue
			}
			onPacket(append([]byte(nil), payload...))
		}
	}()
	return a, nil
}

func (a *audioStream) stop() {
	if a == nil {
		return
	}
	a.once.Do(func() {
		a.p.stop(2 * time.Second)
		a.conn.Close()
		<-a.done
	})
}
