package manager

import (
	"net"
	"time"

	"sync"

	"errors"
	"github.com/shadowsocks/go-shadowsocks2/socks"
	"io"
)

type mode int

const (
	remoteServer mode = iota
	relayClient
	socksClient
)

const udpBufSize = 64 * 1024

type ch chan int

func (self ch) Close() error {
	if self == nil {
		return nil
	}
	select {
	case self <- 1:
	case <-time.After(5 * time.Second): //超时5s
		return errors.New("关闭通道超时")
	}
	select {
	case <-self:
	case <-time.After(5 * time.Second): //超时5s
		return errors.New("关闭通道超时")
	}
	close(self)
	return nil
}

// Listen on addr for encrypted packets and basically do UDP NAT.
func udpRemote(addr string, shadow func(net.PacketConn) net.PacketConn) (io.Closer, error) {
	c, err := net.ListenPacket("udp", addr)
	if err != nil {
		logger.Danger("UDP remote listen error: \n%v", err)
		return nil, err
	}
	c = shadow(c)

	nm := newNATmap(UDPTimeout)
	buf := make([]byte, udpBufSize)
	closed := false
	stop := make(ch)

	logger.Success("listening UDP on %s", addr)
	go func() {
		<-stop
		closed = true
		c.Close()
	}()

	go func() {
		for {
			n, raddr, err := c.ReadFrom(buf)
			if err != nil {
				if closed {
					stop <- 1
					break
				}
				logger.Warning("UDP remote read error: %v", err)
				continue
			}

			tgtAddr := socks.SplitAddr(buf[:n])
			if tgtAddr == nil {
				logger.Warning("failed to split target address from packet: %q", buf[:n])
				continue
			}

			tgtUDPAddr, err := net.ResolveUDPAddr("udp", tgtAddr.String())
			if err != nil {
				logger.Warning("failed to resolve target UDP address: %v", err)
				continue
			}

			payload := buf[len(tgtAddr):n]

			pc := nm.Get(raddr.String())
			if pc == nil {
				pc, err = net.ListenPacket("udp", "")
				if err != nil {
					logger.Warning("UDP remote listen error: %v", err)
					continue
				}

				nm.Add(raddr, c, pc, remoteServer)
			}

			_, err = pc.WriteTo(payload, tgtUDPAddr) // accept only UDPAddr despite the signature
			if err != nil {
				logger.Warning("UDP remote write error: %v", err)
				continue
			}
		}
		defer c.Close()
	}()
	return stop, nil
}

// Packet NAT table
type natmap struct {
	sync.RWMutex
	m       map[string]net.PacketConn
	timeout time.Duration
}

func newNATmap(timeout time.Duration) *natmap {
	m := &natmap{}
	m.m = make(map[string]net.PacketConn)
	m.timeout = timeout
	return m
}

func (m *natmap) Get(key string) net.PacketConn {
	m.RLock()
	defer m.RUnlock()
	return m.m[key]
}

func (m *natmap) Set(key string, pc net.PacketConn) {
	m.Lock()
	defer m.Unlock()

	m.m[key] = pc
}

func (m *natmap) Del(key string) net.PacketConn {
	m.Lock()
	defer m.Unlock()

	pc, ok := m.m[key]
	if ok {
		delete(m.m, key)
		return pc
	}
	return nil
}

func (m *natmap) Add(peer net.Addr, dst, src net.PacketConn, role mode) {
	m.Set(peer.String(), src)

	go func() {
		timedCopy(dst, peer, src, m.timeout, role)
		if pc := m.Del(peer.String()); pc != nil {
			pc.Close()
		}
	}()
}

// copy from src to dst at target with read timeout
func timedCopy(dst net.PacketConn, target net.Addr, src net.PacketConn, timeout time.Duration, role mode) error {
	buf := make([]byte, udpBufSize)

	for {
		src.SetReadDeadline(time.Now().Add(timeout))
		n, raddr, err := src.ReadFrom(buf)
		if err != nil {
			return err
		}

		switch role {
		case remoteServer: // server -> client: add original packet source
			srcAddr := socks.ParseAddr(raddr.String())
			copy(buf[len(srcAddr):], buf[:n])
			copy(buf, srcAddr)
			_, err = dst.WriteTo(buf[:len(srcAddr)+n], target)
		case relayClient: // client -> user: strip original packet source
			srcAddr := socks.SplitAddr(buf[:n])
			_, err = dst.WriteTo(buf[len(srcAddr):n], target)
		case socksClient: // client -> socks5 program: just set RSV and FRAG = 0
			_, err = dst.WriteTo(append([]byte{0, 0, 0}, buf[:n]...), target)
		}

		if err != nil {
			return err
		}
	}
}
