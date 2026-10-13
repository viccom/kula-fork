package collector

import (
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// ListDisks returns the names and persistent IDs of all available disks and
// partitions supported by the collector, sorted by kernel name. It does not
// load configuration or collect metrics, and uses the same duplicate-identity
// checks as monitoring. An empty ID denotes unavailable or ambiguous identity.
func ListDisks() ([]DiskDevice, error) {
	c := &Collector{}
	raw, err := c.readDiskStats(true)
	if err != nil {
		return nil, err
	}
	c.identifyDisks(raw)
	disks := make([]DiskDevice, 0, len(raw))
	for name, dev := range raw {
		disks = append(disks, DiskDevice{Name: name, ID: dev.id})
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks, nil
}

// SeriesKey keeps unidentified disks in a separate namespace from persistent IDs.
func (d DiskDevice) SeriesKey() string {
	if d.ID != "" {
		return d.ID
	}
	return "kernel:" + d.Name
}

// IdentitySource is derived from the stored, scheme-tagged identity.
func (d DiskDevice) IdentitySource() string {
	if d.ID == "" {
		return "kernel"
	}
	source, _, _ := strings.Cut(d.ID, ":")
	return source
}

func (c *Collector) warnDiskOnce(key, format string, args ...any) {
	if c.diskWarnings == nil {
		c.diskWarnings = make(map[string]bool)
	}
	if !c.diskWarnings[key] {
		c.diskWarnings[key] = true
		log.Printf("Warning: "+format, args...)
	}
}

// Fingerprint kinds for cached disk identities. An absent or empty serial
// shares fpNoSerial with a missing one: the identity-relevant payload ("" ) is
// identical, and a serial later gaining a value flips the kind to fpSerial.
const (
	fpNone     uint8 = 0
	fpDiskseq  uint8 = 1
	fpSerial   uint8 = 2
	fpNoSerial uint8 = 3
)

// fpBufSize is large enough for any diskseq or serial sysfs value in a single
// read; sysfs reports these files as 4096 bytes regardless of their content,
// which is why reading them through os.ReadFile would allocate a fresh 4 KiB
// buffer per device per tick.
const fpBufSize = 512

// diskIdentity caches one device's resolved identity so unchanged disks can
// skip the full sysfs re-resolution on subsequent ticks.
type diskIdentity struct {
	fpKind   uint8  // fingerprint kind (fpDiskseq / fpSerial / fpNoSerial)
	fpVal    string // fingerprint payload as read ("" for fpNoSerial)
	baseID   string // raw resolved identity, before duplicate handling and :part suffix
	part     string // partition number suffix ("" for whole disks)
	basePath string // resolved sysfs directory the identity was resolved from
}

// readSysfsInto reads one sysfs file into buf and returns the trimmed payload
// as a sub-slice of buf, allocating nothing. ok is false when the file cannot
// be read or is empty. sysfs single-value files always fit one read.
func readSysfsInto(path string, buf []byte) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	n, err := f.Read(buf)
	_ = f.Close()
	if err != nil && n == 0 {
		return nil, false
	}
	for n > 0 && (buf[n-1] == '\n' || buf[n-1] == ' ' || buf[n-1] == '\t' || buf[n-1] == '\r') {
		n--
	}
	return buf[:n], true
}

// diskFingerprintBuf computes the per-tick change signal for a block device.
// The kernel's diskseq (a monotonic sequence that changes whenever the
// underlying disk is replaced) is preferred; devices without a usable one
// (e.g. an unconnected nbd exposes an empty diskseq file) fall back to
// device/serial, where the attribute's presence, absence, or content is itself
// a stable signal that any identity change invalidates. Steady-state ticks
// allocate nothing: the payload is returned as a sub-slice of buf.
func diskFingerprintBuf(dir string, buf []byte) (uint8, []byte) {
	if v, ok := readSysfsInto(filepath.Join(dir, "diskseq"), buf); ok && len(v) > 0 {
		return fpDiskseq, v
	}
	if v, ok := readSysfsInto(filepath.Join(dir, "device", "serial"), buf); ok && len(v) > 0 {
		return fpSerial, v
	}
	if _, err := os.Stat(filepath.Join(dir, "device", "serial")); err == nil {
		return fpNoSerial, nil // present but empty
	}
	return fpNoSerial, nil
}

// diskParent is one resolved parent-disk identity shared by the disk and its
// partitions in identifyDisks' per-tick duplicate detection.
type diskParent struct{ id, seq string }

// Disk identities follow their device: the fingerprint changes whenever the
// underlying disk is replaced, so a hotplug/replacement or duplicate
// identifier cannot leave a stale name mapping. A device reporting the same
// fingerprint as last tick reuses its cached identity instead of re-reading
// every sysfs attribute — on hosts exposing thousands of block devices (e.g.
// sandboxes that see the host's nbd farm) that re-resolution dominated the
// collector's CPU profile. Unresolved partition layouts are retried every tick.
func (c *Collector) identifyDisks(disks map[string]diskRaw) {
	parents := reuseMap(&c.diskParents)
	paths := reuseMap(&c.diskPaths)
	parts := reuseMap(&c.diskParts)
	counts := reuseMap(&c.diskCounts)
	// The identity cache is read throughout this function, so the per-tick
	// rebuild writes into a swapped buffer instead of clearing the live map.
	cache := reuseMap(&c.diskIDNext)

	var fpBuf [fpBufSize]byte
	for name := range disks {
		base := filepath.Join(sysPath, "class", "block", name)
		kind, payload := diskFingerprintBuf(base, fpBuf[:])
		if cached, ok := c.diskIDCache[name]; ok && cached.basePath != "" &&
			cached.fpKind == kind && string(payload) == cached.fpVal {
			// Unchanged device: reuse the resolved identity and keep it in the
			// duplicate detection so a peer (re)appearing with the same serial
			// is still caught without re-reading every sysfs attribute.
			if _, ok := parents[cached.basePath]; !ok {
				seq := ""
				if kind == fpDiskseq {
					seq = cached.fpVal
				}
				parents[cached.basePath] = diskParent{cached.baseID, seq}
				if cached.baseID != "" {
					counts[cached.baseID]++
				}
			}
			paths[name] = cached.basePath
			if cached.part != "" {
				parts[name] = cached.part
			}
			cache[name] = cached // keep the entry: cache is rebuilt every tick
			continue
		}
		if part := readSysfsFile(filepath.Join(base, "partition")); part != "" {
			// A partition's real sysfs directory is nested beneath its disk.
			resolved, err := filepath.EvalSymlinks(base)
			if n, parseErr := strconv.ParseUint(part, 10, 32); err == nil && parseErr == nil && n > 0 {
				base = filepath.Dir(resolved)
				parts[name] = strconv.FormatUint(n, 10)
			} else {
				continue // identity stays empty; retried on the next tick
			}
		} else if isPartition(name) {
			// Missing partition metadata must never turn into a parent disk ID.
			continue // identity stays empty; retried on the next tick
		} else if resolved, err := filepath.EvalSymlinks(base); err == nil {
			base = resolved
		}
		paths[name] = base
		if _, ok := parents[base]; !ok {
			id := resolveDiskID(base)
			parents[base] = diskParent{id, readSysfsFile(filepath.Join(base, "diskseq"))}
			if id != "" {
				counts[id]++
			}
		}
		cache[name] = diskIdentity{
			fpKind:   kind,
			fpVal:    string(payload),
			baseID:   parents[base].id,
			part:     parts[name],
			basePath: base,
		}
	}
	for name, raw := range disks {
		info := parents[paths[name]]
		raw.diskseq = info.seq
		if info.id != "" && counts[info.id] == 1 {
			raw.id = info.id
			if part := parts[name]; part != "" {
				raw.id += ":part:" + part
			}
		} else if info.id != "" {
			c.warnDiskOnce("duplicate:"+info.id, "duplicate disk identity %q; affected disks use unstable kernel names", info.id)
		}
		disks[name] = raw
	}
	// Swap buffers: the freshly rebuilt cache becomes the read side; the
	// previous one is cleared and reused as next tick's write buffer.
	c.diskIDCache, c.diskIDNext = cache, c.diskIDCache
}

func diskAttribute(base, attribute string) string {
	value := readSysfsFile(filepath.Join(base, attribute))
	if len(value) > 1024 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && !unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	switch strings.ToLower(value) {
	case "", "unknown", "none", "null", "n/a":
		return ""
	}
	if strings.Trim(value, "0 -") == "" {
		return ""
	}
	return value
}

func diskHexID(value string, lengths ...int) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), ""))
	value = strings.ReplaceAll(value, "-", "")
	validLength := false
	for _, n := range lengths {
		validLength = validLength || len(value) == n
	}
	if !validLength || strings.Trim(value, "0") == "" {
		return ""
	}
	for _, ch := range value {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return value
}

func diskWWID(value string) string {
	if value == "" {
		return ""
	}
	// Canonicalize hexadecimal identifiers across the wwid and raw attributes.
	for _, scheme := range []string{"uuid.", "eui.", "naa."} {
		if strings.HasPrefix(strings.ToLower(value), scheme) {
			hex := diskHexID(value[len(scheme):], 16, 32)
			if hex == "" {
				return ""
			}
			return "wwid:" + scheme + hex
		}
	}
	return "wwid:" + diskIDComponent(value)
}

// Escape structural delimiters too: a literal serial "A:part:1" must not
// collide with partition 1 on a disk whose serial is "A".
func diskIDComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func resolveDiskID(base string) string {
	if id := diskWWID(diskAttribute(base, "wwid")); id != "" {
		return id
	}
	for _, field := range []struct {
		name, prefix string
		size         int
	}{{"uuid", "uuid.", 32}, {"nguid", "eui.", 32}, {"eui", "eui.", 16}} {
		if id := diskHexID(diskAttribute(base, field.name), field.size); id != "" {
			return "wwid:" + field.prefix + id
		}
	}
	if id := diskWWID(diskAttribute(base, "device/wwid")); id != "" {
		return id
	}
	serial := diskAttribute(base, "device/serial")
	if serial == "" {
		serial = diskAttribute(base, "serial") // virtio-blk
	}
	if serial == "" {
		return ""
	}
	id := "serial:" + diskIDComponent(diskAttribute(base, "device/vendor")) + "|" +
		diskIDComponent(diskAttribute(base, "device/model")) + "|" + diskIDComponent(serial)
	if strings.HasPrefix(filepath.Base(base), "nvme") {
		// Several namespaces share a controller's serial and model.
		nsid, err := strconv.ParseUint(readSysfsFile(filepath.Join(base, "nsid")), 10, 32)
		if err != nil || nsid == 0 {
			return ""
		}
		id += ":ns:" + strconv.FormatUint(nsid, 10)
	}
	return id
}
