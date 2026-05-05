package pxe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	tftpOpRRQ   = 1
	tftpOpData  = 3
	tftpOpAck   = 4
	tftpOpError = 5
	tftpOpOACK  = 6

	tftpErrNotFound    = 1
	tftpErrAccess      = 2
	tftpErrIllegalOp   = 4
	tftpErrUnknownTID  = 5
	tftpBlockSize      = 512
	tftpMinBlockSize   = 8
	tftpMaxBlockSize   = 65464
	tftpDefaultTimeout = 2 * time.Second
	tftpMaxRetries     = 5
)

type tftpRRQ struct {
	filename string
	mode     string
	options  map[string]string
}

// TFTPServer is a read-only TFTP server for PXE boot assets.
type TFTPServer struct {
	addr       string
	root       string
	timeout    time.Duration
	maxRetries int
}

// NewTFTPServer creates a new read-only TFTP server.
func NewTFTPServer(addr, root string) *TFTPServer {
	if strings.TrimSpace(addr) == "" {
		addr = ":69"
	}
	return &TFTPServer{
		addr:       addr,
		root:       root,
		timeout:    tftpDefaultTimeout,
		maxRetries: tftpMaxRetries,
	}
}

// ListenAndServe starts serving TFTP on UDP.
func (s *TFTPServer) ListenAndServe(ctx context.Context) error {
	pc, err := net.ListenPacket("udp4", s.addr)
	if err != nil {
		return fmt.Errorf("tftp: listen %s: %w", s.addr, err)
	}
	defer pc.Close()

	buf := make([]byte, 1500)
	for {
		if err := pc.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
			return fmt.Errorf("tftp: set read deadline: %w", err)
		}

		n, peer, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			return fmt.Errorf("tftp: read: %w", err)
		}

		peerUDP, ok := peer.(*net.UDPAddr)
		if !ok {
			continue
		}

		rrq, err := parseRRQ(buf[:n])
		if err != nil {
			_ = sendTFTPError(pc, peerUDP, tftpErrIllegalOp, "illegal tftp operation")
			continue
		}

		go s.serveRRQ(peerUDP, rrq)
	}
}

func parseRRQ(pkt []byte) (tftpRRQ, error) {
	if len(pkt) < 4 {
		return tftpRRQ{}, errors.New("packet too short")
	}
	if binary.BigEndian.Uint16(pkt[:2]) != tftpOpRRQ {
		return tftpRRQ{}, errors.New("not rrq")
	}

	parts := make([]string, 0, 4)
	start := 2
	for i := 2; i < len(pkt); i++ {
		if pkt[i] == 0 {
			parts = append(parts, string(pkt[start:i]))
			start = i + 1
		}
	}
	if start != len(pkt) {
		return tftpRRQ{}, errors.New("malformed rrq")
	}
	if len(parts) < 2 {
		return tftpRRQ{}, errors.New("rrq missing filename/mode")
	}

	filename := strings.TrimSpace(parts[0])
	mode := strings.ToLower(strings.TrimSpace(parts[1]))
	if filename == "" {
		return tftpRRQ{}, errors.New("empty filename")
	}
	if mode == "" {
		return tftpRRQ{}, errors.New("empty mode")
	}

	options := map[string]string{}
	if len(parts) > 2 {
		if (len(parts)-2)%2 != 0 {
			return tftpRRQ{}, errors.New("malformed options")
		}
		for i := 2; i < len(parts); i += 2 {
			key := strings.ToLower(strings.TrimSpace(parts[i]))
			val := strings.TrimSpace(parts[i+1])
			if key == "" {
				continue
			}
			options[key] = val
		}
	}

	return tftpRRQ{filename: filename, mode: mode, options: options}, nil
}

func sanitizeTFTPPath(name string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" {
		return "", errors.New("empty filename")
	}
	n = strings.ReplaceAll(n, "\\", "/")
	n = strings.TrimPrefix(n, "/")
	n = path.Clean(n)
	if n == "." || strings.HasPrefix(n, "../") || strings.Contains(n, "/../") {
		return "", errors.New("path traversal")
	}
	if !fs.ValidPath(n) {
		return "", errors.New("invalid path")
	}
	return n, nil
}

func (s *TFTPServer) serveRRQ(peer *net.UDPAddr, rrq tftpRRQ) {
	log.Printf("tftp: rrq peer=%s file=%q mode=%q options=%v", peer, rrq.filename, rrq.mode, rrq.options)

	rel, err := sanitizeTFTPPath(rrq.filename)
	if err != nil {
		log.Printf("tftp: reject invalid path peer=%s file=%q: %v", peer, rrq.filename, err)
		conn, closeErr := net.ListenUDP("udp4", nil)
		if closeErr == nil {
			_ = sendTFTPError(conn, peer, tftpErrAccess, "access denied")
			_ = conn.Close()
		}
		return
	}

	fullPath := filepath.Join(s.root, filepath.FromSlash(rel))
	f, err := os.Open(fullPath)
	if err != nil {
		log.Printf("tftp: open failed peer=%s file=%q: %v", peer, fullPath, err)
		code := tftpErrAccess
		msg := "access denied"
		if errors.Is(err, os.ErrNotExist) {
			code = tftpErrNotFound
			msg = "file not found"
		}
		conn, closeErr := net.ListenUDP("udp4", nil)
		if closeErr == nil {
			_ = sendTFTPError(conn, peer, uint16(code), msg)
			_ = conn.Close()
		}
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		log.Printf("tftp: stat failed peer=%s file=%q: %v", peer, fullPath, err)
		conn, closeErr := net.ListenUDP("udp4", nil)
		if closeErr == nil {
			_ = sendTFTPError(conn, peer, tftpErrAccess, "read error")
			_ = conn.Close()
		}
		return
	}

	dataConn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		log.Printf("tftp: data socket create failed: %v", err)
		return
	}
	defer dataConn.Close()

	blockSize := tftpBlockSize
	acceptedOptions, negotiatedBlockSize := negotiateRRQOptions(rrq.options, stat.Size())
	if len(acceptedOptions) > 0 {
		blockSize = negotiatedBlockSize
		acked := false
		for retry := 0; retry < s.maxRetries; retry++ {
			if err := sendTFTPOACK(dataConn, peer, acceptedOptions); err != nil {
				log.Printf("tftp: send oack failed peer=%s file=%q: %v", peer, fullPath, err)
				return
			}
			ok, err := waitForAck(dataConn, peer, 0, s.timeout)
			if err == nil && ok {
				acked = true
				break
			}
		}
		if !acked {
			log.Printf("tftp: ack timeout after oack peer=%s file=%q", peer, fullPath)
			return
		}
	}

	block := uint16(1)
	buf := make([]byte, blockSize)

	for {
		n, readErr := f.Read(buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			log.Printf("tftp: read failed peer=%s file=%q: %v", peer, fullPath, readErr)
			_ = sendTFTPError(dataConn, peer, tftpErrAccess, "read error")
			return
		}

		chunk := make([]byte, n)
		copy(chunk, buf[:n])

		acked := false
		for retry := 0; retry < s.maxRetries; retry++ {
			if err := sendTFTPData(dataConn, peer, block, chunk); err != nil {
				log.Printf("tftp: send data failed peer=%s file=%q block=%d: %v", peer, fullPath, block, err)
				return
			}
			ok, err := waitForAck(dataConn, peer, block, s.timeout)
			if err == nil && ok {
				acked = true
				break
			}
		}
		if !acked {
			log.Printf("tftp: ack timeout peer=%s file=%q block=%d", peer, fullPath, block)
			return
		}

		if n < blockSize || errors.Is(readErr, io.EOF) {
			log.Printf("tftp: completed peer=%s file=%q", peer, fullPath)
			return
		}
		block++
	}
}

func negotiateRRQOptions(options map[string]string, fileSize int64) (map[string]string, int) {
	accepted := map[string]string{}
	blockSize := tftpBlockSize

	if options == nil {
		return accepted, blockSize
	}

	if _, ok := options["tsize"]; ok {
		accepted["tsize"] = strconv.FormatInt(fileSize, 10)
	}

	if raw, ok := options["blksize"]; ok {
		requested, err := strconv.Atoi(raw)
		if err == nil && requested >= tftpMinBlockSize && requested <= tftpMaxBlockSize {
			blockSize = requested
			accepted["blksize"] = strconv.Itoa(requested)
		}
	}

	return accepted, blockSize
}

func waitForAck(conn *net.UDPConn, peer *net.UDPAddr, block uint16, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	ackBuf := make([]byte, 8)

	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return false, err
		}
		n, from, err := conn.ReadFromUDP(ackBuf)
		if err != nil {
			return false, err
		}
		if !from.IP.Equal(peer.IP) || from.Port != peer.Port {
			log.Printf("tftp: unexpected tid from=%s want=%s block=%d", from, peer, block)
			_ = sendTFTPError(conn, from, tftpErrUnknownTID, "unknown transfer id")
			continue
		}
		if n < 4 {
			continue
		}
		op := binary.BigEndian.Uint16(ackBuf[:2])
		switch op {
		case tftpOpAck:
			return binary.BigEndian.Uint16(ackBuf[2:4]) == block, nil
		case tftpOpError:
			return false, errors.New("peer reported tftp error")
		default:
			continue
		}
	}
}

func sendTFTPData(conn *net.UDPConn, peer *net.UDPAddr, block uint16, payload []byte) error {
	pkt := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(pkt[:2], tftpOpData)
	binary.BigEndian.PutUint16(pkt[2:4], block)
	copy(pkt[4:], payload)
	_, err := conn.WriteToUDP(pkt, peer)
	return err
}

func sendTFTPOACK(conn *net.UDPConn, peer *net.UDPAddr, options map[string]string) error {
	keys := make([]string, 0, len(options))
	for k := range options {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	size := 2
	for _, k := range keys {
		size += len(k) + 1 + len(options[k]) + 1
	}

	pkt := make([]byte, size)
	binary.BigEndian.PutUint16(pkt[:2], tftpOpOACK)
	offset := 2
	for _, k := range keys {
		copy(pkt[offset:], k)
		offset += len(k)
		pkt[offset] = 0
		offset++

		v := options[k]
		copy(pkt[offset:], v)
		offset += len(v)
		pkt[offset] = 0
		offset++
	}

	_, err := conn.WriteToUDP(pkt, peer)
	return err
}

func sendTFTPError(conn net.PacketConn, peer *net.UDPAddr, code uint16, msg string) error {
	payload := []byte(msg)
	pkt := make([]byte, 4+len(payload)+1)
	binary.BigEndian.PutUint16(pkt[:2], tftpOpError)
	binary.BigEndian.PutUint16(pkt[2:4], code)
	copy(pkt[4:], payload)
	pkt[len(pkt)-1] = 0
	_, err := conn.WriteTo(pkt, peer)
	return err
}
