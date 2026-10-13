package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// procStatLine builds a /proc/<pid>/stat record with the fields the monitor
// reads: comm, state, ppid, utime/stime, num_threads, starttime and rss.
func procStatLine(pid int, comm, state string, ppid, utime, stime, threads, starttime, rss int) string {
	return strconv.Itoa(pid) + " (" + comm + ") " + state + " " + strconv.Itoa(ppid) +
		" 1 1 0 -1 4194560 100 0 0 0 " + strconv.Itoa(utime) + " " + strconv.Itoa(stime) + " 0 0 20 0 " +
		strconv.Itoa(threads) + " 0 " + strconv.Itoa(starttime) + " 1000000 " + strconv.Itoa(rss) + " 0"
}

func TestProcessListAndConnections(t *testing.T) {
	p, write := fixture(t)
	write("proc/stat", "cpu  0 0 0 0\nbtime 1000\n")
	write("proc/1/stat", procStatLine(1, "systemd", "S", 0, 5, 5, 3, 999, 42))
	fdDir := filepath.Join(p.proc, "1", "fd")
	if err := os.MkdirAll(fdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("socket:[12345]", filepath.Join(fdDir, "3")); err != nil {
		t.Fatal(err)
	}
	write("proc/net/tcp", "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"+
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 12345 1 0 100 0 0 10 0\n")

	// Baseline CPU accounting: pretend the previous tick was one second ago
	// with zero jiffies so the ten jiffies read now become exactly 10%.
	p.prevJiffies = map[int]uint64{1: 0}
	p.prevJiffiesT = time.Now().Add(-time.Second)

	list := p.Processes()
	if list.Total != 1 || len(list.Processes) != 1 {
		t.Fatalf("process list: %+v", list)
	}
	pi := list.Processes[0]
	if pi.PID != 1 || pi.PPID != 0 || pi.Name != "systemd" || pi.Threads != 3 {
		t.Fatalf("bad process identity: %+v", pi)
	}
	if pi.State != "sleeping" {
		t.Fatalf("state not normalised: %q", pi.State)
	}
	if pi.FDs != 1 || pi.Sockets != 1 {
		t.Fatalf("fd/socket counts: %+v", pi)
	}
	if want := uint64(42 * os.Getpagesize()); pi.MemRSS != want {
		t.Fatalf("rss = %d, want %d", pi.MemRSS, want)
	}
	if pi.Started != 1000+999/userHZ {
		t.Fatalf("start time = %d", pi.Started)
	}
	if pi.CPUPct < 9.9 || pi.CPUPct > 10.1 {
		t.Fatalf("cpu%% = %v, want ~10", pi.CPUPct)
	}

	conns := p.Connections()
	if len(conns.Connections) != 1 {
		t.Fatalf("connections: %+v", conns)
	}
	c := conns.Connections[0]
	if c.Proto != "tcp" || c.LocalAddr != "127.0.0.1" || c.LocalPort != 8080 ||
		c.RemoteAddr != "0.0.0.0" || c.RemotePort != 0 || c.State != "listen" {
		t.Fatalf("bad connection: %+v", c)
	}
	if c.PID != 1 || c.Process != "systemd" {
		t.Fatalf("socket not attributed to its process: %+v", c)
	}

	// The whole table is cached for the refresh window; a second poll must not
	// change what the caller sees.
	if again := p.Processes(); again.Total != 1 {
		t.Fatalf("cache miss: %+v", again)
	}
}

func TestProcessStateNames(t *testing.T) {
	for code, want := range map[string]string{
		"R": "running", "S": "sleeping", "D": "disk_sleep", "I": "idle",
		"Z": "zombie", "T": "stopped", "t": "stopped", "X": "dead", "?": "unknown",
	} {
		if got := processState(code); got != want {
			t.Errorf("state %q = %q, want %q", code, got, want)
		}
	}
}
