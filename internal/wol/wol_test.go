package wol

import (
	"bytes"
	"testing"
)

func TestMagicPacket(t *testing.T) {
	p, err := MagicPacket("BC:24:11:01:02:03")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 || !bytes.Equal(p[:6], bytes.Repeat([]byte{0xff}, 6)) || !bytes.Equal(p[96:], []byte{0xbc, 0x24, 0x11, 1, 2, 3}) {
		t.Fatalf("bad packet % x", p)
	}
	if _, err := MagicPacket("not-a-mac"); err == nil {
		t.Fatal("expected error")
	}
}
