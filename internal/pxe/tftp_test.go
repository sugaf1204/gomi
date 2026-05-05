package pxe

import (
	"encoding/binary"
	"testing"
)

func TestParseRRQ(t *testing.T) {
	pkt := make([]byte, 0, 64)
	op := make([]byte, 2)
	binary.BigEndian.PutUint16(op, tftpOpRRQ)
	pkt = append(pkt, op...)
	pkt = append(pkt, []byte("pxelinux.0")...)
	pkt = append(pkt, 0)
	pkt = append(pkt, []byte("octet")...)
	pkt = append(pkt, 0)
	pkt = append(pkt, []byte("tsize")...)
	pkt = append(pkt, 0)
	pkt = append(pkt, []byte("0")...)
	pkt = append(pkt, 0)

	rrq, err := parseRRQ(pkt)
	if err != nil {
		t.Fatalf("parseRRQ: %v", err)
	}
	if rrq.filename != "pxelinux.0" {
		t.Fatalf("unexpected filename: %q", rrq.filename)
	}
	if rrq.mode != "octet" {
		t.Fatalf("unexpected mode: %q", rrq.mode)
	}
	if rrq.options["tsize"] != "0" {
		t.Fatalf("unexpected tsize option: %q", rrq.options["tsize"])
	}
}

func TestParseRRQ_Invalid(t *testing.T) {
	if _, err := parseRRQ([]byte{0x00, 0x02, 0x00}); err == nil {
		t.Fatal("expected parse error for non-RRQ packet")
	}
}

func TestNegotiateRRQOptions(t *testing.T) {
	opts, blockSize := negotiateRRQOptions(map[string]string{
		"tsize":   "0",
		"blksize": "1468",
	}, 12345)
	if blockSize != 1468 {
		t.Fatalf("unexpected block size: %d", blockSize)
	}
	if opts["tsize"] != "12345" {
		t.Fatalf("unexpected tsize: %q", opts["tsize"])
	}
	if opts["blksize"] != "1468" {
		t.Fatalf("unexpected blksize: %q", opts["blksize"])
	}
}

func TestSanitizeTFTPPath(t *testing.T) {
	rel, err := sanitizeTFTPPath("debian/linux")
	if err != nil {
		t.Fatalf("sanitizeTFTPPath valid path: %v", err)
	}
	if rel != "debian/linux" {
		t.Fatalf("unexpected sanitized path: %q", rel)
	}

	if _, err := sanitizeTFTPPath("../../etc/passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
