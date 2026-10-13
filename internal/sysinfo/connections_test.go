package sysinfo

import "testing"

func TestDecodeHexAddr(t *testing.T) {
	cases := []struct {
		in   string
		addr string
		port int
	}{
		{"0100007F:1F90", "127.0.0.1", 8080},
		{"00000000:0000", "0.0.0.0", 0},
		{"0201A8C0:0050", "192.168.1.2", 80},
		// ::1 in the kernel's per-word little-endian encoding.
		{"00000000000000000000000001000000:1F90", "::1", 8080},
	}
	for _, tc := range cases {
		addr, port := decodeHexAddr(tc.in)
		if addr != tc.addr || port != tc.port {
			t.Errorf("decodeHexAddr(%q) = %q:%d, want %q:%d", tc.in, addr, port, tc.addr, tc.port)
		}
	}
}

func TestConnectionStateNames(t *testing.T) {
	for code, want := range map[string]string{
		"01": "established", "0A": "listen", "06": "time_wait",
		"08": "close_wait", "07": "close", "FF": "unknown",
	} {
		if got := connectionState(code); got != want {
			t.Errorf("state %q = %q, want %q", code, got, want)
		}
	}
}
