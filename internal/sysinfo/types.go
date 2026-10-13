// Package sysinfo discovers current Linux hardware without adding inventory to
// metric samples or the historical storage format. All discovery is best effort.
package sysinfo

import "time"

// Details contains named hardware attributes. Missing attributes are omitted;
// an unreadable sensor or counter is never represented as a measured zero.
type Details map[string]string

type Snapshot struct {
	Timestamp   time.Time    `json:"ts"`
	MetricsTime *time.Time   `json:"metrics_ts,omitempty"`
	System      Details      `json:"system"`
	CPU         CPU          `json:"cpu"`
	Disks       []Disk       `json:"disks"`
	Filesystems []Filesystem `json:"filesystems"`
	Network     []Interface  `json:"network"`
	PCI         []Details    `json:"pci"`
	USB         []Details    `json:"usb"`
	Sensors     []Sensor     `json:"sensors"`
	Power       []Details    `json:"power"`
	Live        *Live        `json:"live,omitempty"`
}

// Live contains only the values from the latest in-memory sample presented by
// the inventory page. It is never filled from historical storage.
type Live struct {
	Memory  LiveMemory  `json:"mem"`
	System  LiveSystem  `json:"sys"`
	CPU     LiveCPU     `json:"cpu"`
	Hottest *LiveSensor `json:"hottest,omitempty"`
	GPU     []LiveGPU   `json:"gpu,omitempty"`
	// FileSystems carries the per-mount capacity measured by the regular
	// collector. It stays in the live section (rather than the gated hardware
	// detail sections) so the dashboard can show disk usage without exposing
	// the rest of the host inventory.
	FileSystems []LiveFilesystem `json:"filesystems,omitempty"`
}

// LiveFilesystem is one mounted filesystem's capacity as measured by the
// latest sample: the mount point, its type and the used/total/available
// figures. The backing device is deliberately omitted so the live section
// keeps the details-off promise (no host inventory survives), while still
// answering the operator's "how full is each disk" question.
type LiveFilesystem struct {
	Mount     string  `json:"mount"`
	Type      string  `json:"type,omitempty"`
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	UsedPct   float64 `json:"used_pct"`
}

// ProcessInfo describes one process read from /proc. It is collected on
// demand for the process monitor page, never stored in metric history.
type ProcessInfo struct {
	PID     int     `json:"pid"`
	PPID    int     `json:"ppid"`
	Name    string  `json:"name"`
	User    string  `json:"user,omitempty"`
	State   string  `json:"state"`
	Threads int     `json:"threads"`
	CPUPct  float64 `json:"cpu_pct"`
	MemRSS  uint64  `json:"mem_rss"`
	FDs     int     `json:"fds"`
	Sockets int     `json:"sockets"`
	Started int64   `json:"started,omitempty"` // unix seconds, 0 when unknown
}

// ProcessList is the process monitor payload.
type ProcessList struct {
	Timestamp time.Time     `json:"ts"`
	Total     int           `json:"total"`
	Processes []ProcessInfo `json:"processes"`
}

// Connection describes one socket from /proc/net. Local and remote addresses
// are kept as strings so IPv4 and IPv6 share one shape; ports are separate so
// the interface can filter and sort numerically.
type Connection struct {
	Proto      string `json:"proto"`
	LocalAddr  string `json:"local_addr"`
	LocalPort  int    `json:"local_port"`
	RemoteAddr string `json:"remote_addr"`
	RemotePort int    `json:"remote_port"`
	State      string `json:"state"`
	PID        int    `json:"pid,omitempty"`
	Process    string `json:"process,omitempty"`
}

// ConnectionList is the network monitor payload.
type ConnectionList struct {
	Timestamp   time.Time    `json:"ts"`
	Connections []Connection `json:"connections"`
}

type LiveMemory struct {
	Total   uint64  `json:"total"`
	Used    uint64  `json:"used"`
	UsedPct float64 `json:"used_pct"`
}

type LiveSystem struct {
	UptimeHuman string `json:"uptime_human"`
	Processes   int    `json:"processes"`
}

type LiveCPU struct {
	UsagePct float64 `json:"usage_pct"`
	Load1    float64 `json:"load1"`
	Load5    float64 `json:"load5"`
	Load15   float64 `json:"load15"`
}

// LiveSensor identifies the warmest temperature the kernel currently reports.
type LiveSensor struct {
	Device string  `json:"device"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
}

type LiveGPU struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
}

type CPU struct {
	ModelName   string `json:"model_name,omitempty"`
	LogicalCPUs int    `json:"logical_cpus"`
	Cores       int    `json:"cores,omitempty"`
}

// Disk class values. They are stable identifiers; the interface translates them.
const (
	ClassDisk       = "disk"
	ClassPartition  = "partition"
	ClassVirtual    = "virtual"
	ClassLoop       = "loop"
	ClassCompressed = "compressed"
)

// Medium values describe how a physical disk stores data.
const (
	MediumHDD = "hdd"
	MediumSSD = "ssd"
)

type Disk struct {
	Name    string   `json:"name"`
	Details Details  `json:"details"`
	Size    *uint64  `json:"size_bytes,omitempty"`
	Parent  string   `json:"parent,omitempty"`
	Slaves  []string `json:"-"`
	Mounts  []string `json:"mounts"`
	// Class separates real drives from the partitions, LVM/crypt mappings, loop
	// images and compressed RAM devices that share the same block namespace.
	Class string `json:"class,omitempty"`
	// Medium is hdd or ssd for physical drives and empty for stacked devices.
	Medium string `json:"medium,omitempty"`
	// Tracked reports whether the regular collector stores this device's history.
	Tracked bool `json:"tracked"`
}

type Filesystem struct {
	Device   string           `json:"device"`
	Mount    string           `json:"mount"`
	Type     string           `json:"type"`
	DeviceID string           `json:"-"`
	Usage    *FilesystemUsage `json:"usage,omitempty"`
	// Tracked reports whether the regular collector stores this mount's history.
	Tracked bool `json:"tracked"`
}

type FilesystemUsage struct {
	Total     uint64  `json:"total"`
	Used      uint64  `json:"used"`
	Available uint64  `json:"available"`
	UsedPct   float64 `json:"used_pct"`
}

// Interface kinds. They are stable identifiers; the interface translates them.
const (
	KindWired    = "wired"
	KindWireless = "wireless"
	KindVirtual  = "virtual"
	KindLocal    = "local"
)

// Interface describes a network interface by kernel name only. IP addresses and
// MAC addresses are deliberately not collected: the interface name, kind, link
// state, MTU, and speed answer "is this host connected" without turning the
// inventory into an address book of the machine.
type Interface struct {
	Name      string  `json:"name"`
	Details   Details `json:"details"`
	SpeedMbps *uint64 `json:"speed_mbps,omitempty"`
	MTU       int     `json:"mtu,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	// Tracked reports whether the regular collector stores this interface's history.
	Tracked bool `json:"tracked"`
}

// Sensor kinds. They are stable identifiers; the interface translates them.
const (
	SensorTemperature = "temperature"
	SensorFan         = "fan"
	SensorVoltage     = "voltage"
	SensorCurrent     = "current"
	SensorPower       = "power"
	SensorHumidity    = "humidity"
)

type Sensor struct {
	Device string  `json:"device"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Kind   string  `json:"kind,omitempty"`
}
