package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// parseFocusedMon must take the monitor name from focusedmon and reject every
// other event, including focusedmonv2, so a Hyprland that drops v1 fails here
// rather than silently serving a stale monitor.
func TestParseFocusedMon(t *testing.T) {
	cases := []struct {
		line string
		mon  string
		ok   bool
	}{
		{"focusedmon>>DP-1,3", "DP-1", true},
		{"focusedmon>>HDMI-A-1,name with spaces", "HDMI-A-1", true},
		{"focusedmon>>DP-1", "DP-1", true},
		{"focusedmonv2>>DP-1,3", "", false},
		{"workspace>>3", "", false},
		{"focusedmon>>", "", false},
		{"focusedmon>>,3", "", false},
		{"garbage", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		mon, ok := parseFocusedMon(c.line)
		if mon != c.mon || ok != c.ok {
			t.Fatalf("parseFocusedMon(%q) = (%q,%v), want (%q,%v)", c.line, mon, ok, c.mon, c.ok)
		}
	}
}

func TestParseMonitorRemoved(t *testing.T) {
	cases := []struct {
		line string
		name string
		ok   bool
	}{
		{"monitorremoved>>DP-1", "DP-1", true},
		{"monitoradded>>DP-1", "", false},
		{"monitorremovedv2>>1,DP-1", "", false},
		{"monitorremoved>>", "", false},
		{"focusedmon>>DP-1,3", "", false},
	}
	for _, c := range cases {
		name, ok := parseMonitorRemoved(c.line)
		if name != c.name || ok != c.ok {
			t.Fatalf("parseMonitorRemoved(%q) = (%q,%v), want (%q,%v)", c.line, name, ok, c.name, c.ok)
		}
	}
}

// an empty write must never clobber a known monitor: a failed seed on reconnect
// should leave the cache as-is, degrading to a fresh query, not a wrong one.
func TestSetMonitorEmptyNoClobber(t *testing.T) {
	d := &daemon{}
	d.setMonitor("DP-1")
	d.setMonitor("")
	if got := d.cachedMonitor(); got != "DP-1" {
		t.Fatalf("cachedMonitor() = %q, want DP-1", got)
	}
}

// a warm cache must answer without touching the fallback: this is the whole
// point, removing the per-keybind subprocess.
func TestActiveMonitorCacheShortCircuits(t *testing.T) {
	calls := 0
	d := &daemon{monFallback: func() string { calls++; return "FALLBACK" }}
	d.setMonitor("DP-2")
	if got := d.activeMonitor(); got != "DP-2" {
		t.Fatalf("activeMonitor() = %q, want DP-2", got)
	}
	if calls != 0 {
		t.Fatalf("fallback called %d times on a warm cache, want 0", calls)
	}
}

// a cold cache must reach the fallback so correctness never depends on a warm
// cache.
func TestActiveMonitorColdFallback(t *testing.T) {
	calls := 0
	d := &daemon{monFallback: func() string { calls++; return "FALLBACK" }}
	if got := d.activeMonitor(); got != "FALLBACK" {
		t.Fatalf("activeMonitor() = %q, want FALLBACK", got)
	}
	if calls != 1 {
		t.Fatalf("fallback called %d times on a cold cache, want 1", calls)
	}
}

// consume must update on focusedmon, ignore unrelated lines, leave the cache on
// an unrelated removal, and clear it only when the cached monitor is removed.
func TestConsumeHyprEvents(t *testing.T) {
	d := &daemon{}
	d.consumeHyprEvents(strings.NewReader("workspace>>1\nfocusedmon>>DP-1,2\nactivewindow>>a,b\n"))
	if got := d.cachedMonitor(); got != "DP-1" {
		t.Fatalf("after focusedmon, cachedMonitor() = %q, want DP-1", got)
	}
	d.consumeHyprEvents(strings.NewReader("monitorremoved>>HDMI-A-1\n"))
	if got := d.cachedMonitor(); got != "DP-1" {
		t.Fatalf("after unrelated removal, cachedMonitor() = %q, want DP-1", got)
	}
	d.consumeHyprEvents(strings.NewReader("focusedmon>>HDMI-A-1,5\nmonitorremoved>>HDMI-A-1\n"))
	if got := d.cachedMonitor(); got != "" {
		t.Fatalf("after removing the cached monitor, cachedMonitor() = %q, want empty", got)
	}
}

func TestHyprSocket2Path(t *testing.T) {
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if got := hyprSocket2Path(); got != "" {
		t.Fatalf("with no instance signature, path = %q, want empty", got)
	}

	rt := t.TempDir()
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "sig")
	t.Setenv("XDG_RUNTIME_DIR", rt)
	want := filepath.Join(rt, "hypr", "sig", ".socket2.sock")
	if got := hyprSocket2Path(); got != want {
		t.Fatalf("with no socket yet, path = %q, want %q", got, want)
	}
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := hyprSocket2Path(); got != want {
		t.Fatalf("with the socket present, path = %q, want %q", got, want)
	}
}

// the cache must be safe under the watcher writing while keybinds read; run with
// -race to prove it.
func TestMonitorConcurrent(t *testing.T) {
	d := &daemon{}
	done := make(chan struct{})
	go func() {
		for i := 0; i < 2000; i++ {
			d.setMonitor("DP-1")
			d.clearMonitor()
		}
		close(done)
	}()
	for i := 0; i < 2000; i++ {
		_ = d.cachedMonitor()
	}
	<-done
}

func BenchmarkActiveMonitorCached(b *testing.B) {
	d := &daemon{}
	d.setMonitor("DP-1")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if d.activeMonitor() == "" {
			b.Fatal("empty monitor")
		}
	}
}

// BenchmarkMonitorSpawnProxy measures a bare fork+exec: the floor of the cost
// the cache removes from every keybind. The hyprctl call it replaces is
// strictly more expensive (a unix socket round-trip plus JSON decode on top).
func BenchmarkMonitorSpawnProxy(b *testing.B) {
	if _, err := exec.LookPath("true"); err != nil {
		b.Skip("true not on PATH")
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = exec.Command("true").Run()
	}
}
