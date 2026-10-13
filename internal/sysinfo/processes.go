package sysinfo

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// monitorRefresh bounds how often the process/connection tables are rebuilt.
	// The monitor pages poll, and rebuilding this scan for every poll of every
	// client would multiply the /proc traffic for no fresher data than the UI
	// shows.
	monitorRefresh = 2 * time.Second
	// userHZ is Linux's USER_HZ (getconf CLK_TCK). Process CPU time and start
	// times in /proc are expressed in these ticks; 100 on every mainstream
	// configuration.
	userHZ = 100
)

// procTable is one scan of /proc shared by the process list and by the
// socket→process attribution on the connection list: the file-descriptor walk
// that counts a process's sockets also records which pid owns each socket
// inode, so the connection list does not need a second sweep.
type procTable struct {
	ts        time.Time
	processes []ProcessInfo
	inode     map[uint64]int
	names     map[int]string
}

// Processes returns the process monitor payload, rebuilt at most every
// monitorRefresh.
func (p *Provider) Processes() *ProcessList {
	p.procMu.Lock()
	defer p.procMu.Unlock()
	table := p.table(time.Now())
	return &ProcessList{Timestamp: table.ts, Total: len(table.processes), Processes: table.processes}
}

// table returns the shared /proc scan, rebuilding it no more often than
// monitorRefresh. Callers must hold procMu.
func (p *Provider) table(now time.Time) *procTable {
	if p.procTable != nil && now.Sub(p.procTable.ts) < monitorRefresh {
		return p.procTable
	}
	p.procTable = p.scanProc(now)
	return p.procTable
}

func (p *Provider) scanProc(now time.Time) *procTable {
	table := &procTable{ts: now, inode: map[uint64]int{}, names: map[int]string{}, processes: []ProcessInfo{}}
	entries, err := os.ReadDir(p.proc)
	if err != nil {
		return table
	}
	users := p.userNames()
	btime := p.bootTime()
	pageSize := uint64(os.Getpagesize())
	elapsed := now.Sub(p.prevJiffiesT).Seconds()
	if p.prevJiffiesT.IsZero() || elapsed <= 0 {
		elapsed = 0
	}
	next := make(map[int]uint64, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		info, jiffies, ok := p.readProcess(pid, entry, users, pageSize, btime)
		if !ok {
			continue
		}
		if elapsed > 0 {
			if prev, seen := p.prevJiffies[pid]; seen && jiffies >= prev {
				info.CPUPct = round2(float64(jiffies-prev) / userHZ / elapsed * 100)
			}
		}
		next[pid] = jiffies
		info.FDs, info.Sockets = p.scanFDs(pid, table.inode)
		table.processes = append(table.processes, info)
		table.names[pid] = info.Name
	}
	p.prevJiffies = next
	p.prevJiffiesT = now
	sort.Slice(table.processes, func(i, j int) bool { return table.processes[i].PID < table.processes[j].PID })
	return table
}

// readProcess reads one /proc/<pid>/stat record. The comm field may contain
// spaces and parentheses, so the integer fields are read from the tokens that
// follow the final ')'.
func (p *Provider) readProcess(pid int, entry os.DirEntry, users map[uint32]string, pageSize uint64, btime int64) (ProcessInfo, uint64, bool) {
	data, err := os.ReadFile(filepath.Join(p.proc, entry.Name(), "stat"))
	if err != nil {
		return ProcessInfo{}, 0, false
	}
	open := bytes.IndexByte(data, '(')
	closing := bytes.LastIndexByte(data, ')')
	if open < 0 || closing < open {
		return ProcessInfo{}, 0, false
	}
	info := ProcessInfo{PID: pid, Name: string(data[open+1 : closing])}
	rest := strings.Fields(string(data[closing+1:]))
	// field 3 = state, 4 = ppid, 14 = utime, 15 = stime, 20 = num_threads,
	// 22 = starttime, 24 = rss (pages). After the ')' they are tokens 0..21.
	if len(rest) < 22 {
		return ProcessInfo{}, 0, false
	}
	info.State = processState(rest[0])
	info.PPID = atoi(rest[1])
	utime, stime := atou(rest[11]), atou(rest[12])
	info.Threads = atoi(rest[17])
	info.MemRSS = atou(rest[21]) * pageSize
	if btime > 0 {
		info.Started = btime + int64(atou(rest[19])/userHZ)
	}
	if fi, err := entry.Info(); err == nil {
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			info.User = lookupUser(users, st.Uid)
		}
	}
	return info, utime + stime, true
}

// scanFDs counts a process's open file descriptors and sockets, recording the
// owning pid of every socket inode it finds so the connection list can be
// attributed without a second /proc walk.
func (p *Provider) scanFDs(pid int, inode map[uint64]int) (fds int, sockets int) {
	dir := filepath.Join(p.proc, strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		if rest, ok := strings.CutPrefix(target, "socket:["); ok {
			if n, err := strconv.ParseUint(strings.TrimSuffix(rest, "]"), 10, 64); err == nil {
				inode[n] = pid
			}
			sockets++
		}
	}
	return len(entries), sockets
}

// userNames maps uid to login name from /etc/passwd, cached for the process's
// lifetime (the file does not change under a running monitor often enough to
// matter). An unknown uid is rendered as its number by lookupUser.
func (p *Provider) userNames() map[uint32]string {
	if p.uidNames != nil {
		return p.uidNames
	}
	names := map[uint32]string{}
	if data, err := os.ReadFile("/etc/passwd"); err == nil {
		for line := range strings.SplitSeq(string(data), "\n") {
			fields := strings.Split(line, ":")
			if len(fields) >= 3 {
				if uid, err := strconv.ParseUint(fields[2], 10, 32); err == nil {
					names[uint32(uid)] = fields[0]
				}
			}
		}
	}
	p.uidNames = names
	return names
}

func lookupUser(users map[uint32]string, uid uint32) string {
	if name, ok := users[uid]; ok {
		return name
	}
	return strconv.FormatUint(uint64(uid), 10)
}

// bootTime returns the epoch seconds of the last boot (btime in /proc/stat),
// used to translate a process's starttime ticks into a wall-clock instant.
func (p *Provider) bootTime() int64 {
	data, err := os.ReadFile(filepath.Join(p.proc, "stat"))
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "btime "); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			return n
		}
	}
	return 0
}

// processState normalises the single-letter /proc state code into a stable slug
// the interface translates.
func processState(code string) string {
	if code == "" {
		return "unknown"
	}
	switch code[0] {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'D':
		return "disk_sleep"
	case 'I':
		return "idle"
	case 'Z':
		return "zombie"
	case 'T', 't':
		return "stopped"
	case 'X', 'x':
		return "dead"
	case 'W':
		return "paging"
	case 'P':
		return "parked"
	}
	return "unknown"
}

func atoi(s string) int        { n, _ := strconv.Atoi(s); return n }
func atou(s string) uint64     { n, _ := strconv.ParseUint(s, 10, 64); return n }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
