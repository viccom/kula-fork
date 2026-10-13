package sysinfo

import (
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// netProtocols are the /proc/net tables the network monitor reads. Unix domain
// sockets are deliberately excluded: their "address" is an opaque path, not a
// host/port pair, and they would double the row count without adding anything
// an operator can act on.
var netProtocols = []string{"tcp", "tcp6", "udp", "udp6"}

// Connections returns the socket list with the owning process resolved from the
// shared /proc scan.
func (p *Provider) Connections() *ConnectionList {
	p.procMu.Lock()
	table := p.table(time.Now())
	p.procMu.Unlock()
	list := &ConnectionList{Timestamp: table.ts, Connections: []Connection{}}
	for _, proto := range netProtocols {
		list.Connections = append(list.Connections, p.parseNetFile(proto, table)...)
	}
	sort.SliceStable(list.Connections, func(i, j int) bool {
		a, b := list.Connections[i], list.Connections[j]
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.LocalPort != b.LocalPort {
			return a.LocalPort < b.LocalPort
		}
		if a.LocalAddr != b.LocalAddr {
			return a.LocalAddr < b.LocalAddr
		}
		return a.RemotePort < b.RemotePort
	})
	return list
}

// parseNetFile parses one /proc/net/<proto> table.
func (p *Provider) parseNetFile(proto string, table *procTable) []Connection {
	data, err := os.ReadFile(filepath.Join(p.proc, "net", proto))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]Connection, 0, len(lines))
	for i, line := range lines {
		if i == 0 {
			continue // header
		}
		fields := strings.Fields(line)
		// sl local rem st tx:rx tr:tm retrnsmt uid timeout inode …
		if len(fields) < 10 {
			continue
		}
		localAddr, localPort := decodeHexAddr(fields[1])
		remoteAddr, remotePort := decodeHexAddr(fields[2])
		conn := Connection{
			Proto: proto, LocalAddr: localAddr, LocalPort: localPort,
			RemoteAddr: remoteAddr, RemotePort: remotePort, State: connectionState(fields[3]),
		}
		if inode, err := strconv.ParseUint(fields[9], 10, 64); err == nil {
			if pid, ok := table.inode[inode]; ok {
				conn.PID = pid
				conn.Process = table.names[pid]
			}
		}
		out = append(out, conn)
	}
	return out
}

// decodeHexAddr turns the kernel's "HEXADDR:HEXPORT" notation into a printable
// address and a numeric port. The address bytes are little-endian within each
// 32-bit word, so both IPv4 and IPv6 words are byte-reversed before parsing.
func decodeHexAddr(field string) (string, int) {
	ipHex, portHex, ok := strings.Cut(field, ":")
	if !ok {
		return "", 0
	}
	port, _ := strconv.ParseUint(portHex, 16, 16)
	raw, err := hex.DecodeString(ipHex)
	if err != nil {
		return "", int(port)
	}
	switch len(raw) {
	case 4:
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]).String(), int(port)
	case 16:
		for i := 0; i < 16; i += 4 {
			raw[i], raw[i+3] = raw[i+3], raw[i]
			raw[i+1], raw[i+2] = raw[i+2], raw[i+1]
		}
		return net.IP(raw).String(), int(port)
	}
	return "", int(port)
}

// connectionState maps the kernel's two-digit hex TCP state to a stable slug.
func connectionState(code string) string {
	switch strings.ToUpper(code) {
	case "01":
		return "established"
	case "02":
		return "syn_sent"
	case "03":
		return "syn_recv"
	case "04":
		return "fin_wait1"
	case "05":
		return "fin_wait2"
	case "06":
		return "time_wait"
	case "07":
		return "close"
	case "08":
		return "close_wait"
	case "09":
		return "last_ack"
	case "0A":
		return "listen"
	case "0B":
		return "closing"
	case "0C":
		return "new_syn_recv"
	}
	return "unknown"
}
