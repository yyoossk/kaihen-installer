package main

import "testing"

// Live resolution checks for the Rodpad catalog entries (SW2NES,
// SW2SNES, SW2Transfer, SW2PS2): each must resolve its newest
// release and its single .nro asset.
func TestRodpadResolves(t *testing.T) {
	cases := []struct{ id, tag, nro string }{
		{"Rodpad/SW2NES", "v2.12.2", "sw2nes.nro"},
		{"Rodpad/SW2SNES", "v1.0.1", "sw2snes.nro"},
		{"Rodpad/SW2Transfer", "v2.0.0", "sw2transfer.nro"},
		{"Rodpad/SW2PS2", "v1.1.0", "sw2ps2.nro"},
	}
	for _, c := range cases {
		a := &appEntry{ID: c.id, Kind: "nro", Source: "release"}
		tag, assets, err := fetchRelease(a)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if tag != c.tag {
			t.Fatalf("%s: tag = %q, want %q", c.id, tag, c.tag)
		}
		found := false
		for _, as := range assets {
			if as.Name == c.nro {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: %s not among assets: %v", c.id, c.nro, assets)
		}
	}
}
