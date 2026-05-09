package dns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"codeberg.org/miekg/dns/rdata"
)

type dynamicRecordSet struct {
	TTL    uint32
	Values []string
}

type dynamicRecordFile struct {
	Records []dynamicRecordEntry `json:"records"`
}

type dynamicRecordEntry struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	TTL    uint32   `json:"ttl"`
	Values []string `json:"values"`
}

func (s *EmbeddedServer) queryRecordsLocked(name string, qType uint16) ([]dnsv2.RR, bool) {
	answers := []dnsv2.RR{}
	known := false

	if ips, ok := s.records[name]; ok {
		known = true
		if qType == dnsv2.TypeA || qType == dnsv2.TypeANY {
			for _, ip := range ips {
				answers = append(answers, &dnsv2.A{
					Hdr: dnsv2.Header{Name: name, Class: dnsv2.ClassINET, TTL: s.ttl},
					A:   rdata.A{Addr: ip},
				})
			}
		}
	}

	if zone, ok := s.zoneForNameLocked(name); ok && name == zone {
		known = true
		switch qType {
		case dnsv2.TypeSOA:
			answers = append(answers, s.soaRR(zone))
		case dnsv2.TypeNS:
			answers = append(answers, s.nsRR(zone))
		case dnsv2.TypeANY:
			answers = append(answers, s.soaRR(zone), s.nsRR(zone))
		}
	}

	if sets, ok := s.dynamicRecords[name]; ok {
		known = true
		for rrType, set := range sets {
			if qType != dnsv2.TypeANY && qType != rrType {
				continue
			}
			answers = append(answers, dynamicRRs(name, rrType, set)...)
		}
	}

	return answers, known
}

func (s *EmbeddedServer) serveDynamicUpdate(w dnsv2.ResponseWriter, req *dnsv2.Msg) {
	resp := new(dnsv2.Msg)
	dnsutil.SetReply(resp, req)
	resp.Authoritative = true

	if len(req.Data) > 0 {
		req.Options = dnsv2.MsgOptionUnpack
		if err := req.Unpack(); err != nil {
			resp.Rcode = dnsv2.RcodeFormatError
			writeDNSResponse(w, resp)
			return
		}
	}

	zone, ok := updateZone(req)
	if !ok {
		resp.Rcode = dnsv2.RcodeFormatError
		writeDNSResponse(w, resp)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.zones[zone]; !ok {
		resp.Rcode = dnsv2.RcodeNotZone
		writeDNSResponse(w, resp)
		return
	}
	if !s.dynamicLoaded {
		if err := s.loadDynamicRecordsLocked(); err != nil {
			resp.Rcode = dnsv2.RcodeServerFailure
			writeDNSResponse(w, resp)
			return
		}
		s.dynamicLoaded = true
	}

	for _, rr := range req.Ns {
		if err := s.applyDynamicUpdateLocked(zone, rr); err != nil {
			resp.Rcode = updateErrorCode(err)
			writeDNSResponse(w, resp)
			return
		}
	}
	if err := s.persistDynamicRecordsLocked(); err != nil {
		log.Printf("dns: persist dynamic records: %v", err)
		resp.Rcode = dnsv2.RcodeServerFailure
		writeDNSResponse(w, resp)
		return
	}

	s.serialAt = nowUTC()
	writeDNSResponse(w, resp)
}

func (s *EmbeddedServer) applyDynamicUpdateLocked(zone string, rr dnsv2.RR) error {
	hdr := rr.Header()
	name, ok := canonicalFQDN(hdr.Name)
	if !ok {
		return errDynamicFormat
	}
	if !nameInZone(name, zone) {
		return errDynamicNotZone
	}
	rrType := dnsv2.RRToType(rr)

	switch hdr.Class {
	case dnsv2.ClassANY:
		if rrType == dnsv2.TypeANY {
			delete(s.dynamicRecords, name)
			return nil
		}
		deleteDynamicRecordSet(s.dynamicRecords, name, rrType)
		return nil
	case dnsv2.ClassNONE:
		value, ok := dynamicRecordValue(rr)
		if !ok {
			return errDynamicUnsupported
		}
		deleteDynamicRecordValue(s.dynamicRecords, name, rrType, value)
		return nil
	case dnsv2.ClassINET:
		value, ok := dynamicRecordValue(rr)
		if !ok {
			return errDynamicUnsupported
		}
		if hdr.TTL == 0 {
			hdr.TTL = s.ttl
		}
		addDynamicRecordValue(s.dynamicRecords, name, rrType, hdr.TTL, value)
		return nil
	default:
		return errDynamicFormat
	}
}

func (s *EmbeddedServer) serveAXFR(ctx context.Context, w dnsv2.ResponseWriter, req *dnsv2.Msg, zone string) {
	s.mu.RLock()
	if _, ok := s.zones[zone]; !ok {
		s.mu.RUnlock()
		resp := new(dnsv2.Msg)
		dnsutil.SetReply(resp, req)
		resp.Authoritative = true
		resp.Rcode = dnsv2.RcodeRefused
		writeDNSResponse(w, resp)
		return
	}
	rrs := s.zoneRecordsLocked(zone)
	s.mu.RUnlock()

	w.Hijack()
	env := make(chan *dnsv2.Envelope)
	go func() {
		defer close(env)
		for _, rr := range rrs {
			select {
			case <-ctx.Done():
				return
			case env <- &dnsv2.Envelope{Answer: []dnsv2.RR{rr}}:
			}
		}
	}()
	client := dnsv2.NewClient()
	if err := client.TransferOut(w, req, env); err != nil {
		log.Printf("dns: axfr %s: %v", zone, err)
	}
	_ = w.Close()
}

func (s *EmbeddedServer) zoneRecordsLocked(zone string) []dnsv2.RR {
	rrs := []dnsv2.RR{s.soaRR(zone), s.nsRR(zone)}

	names := make([]string, 0, len(s.records)+len(s.dynamicRecords))
	seen := map[string]struct{}{}
	for name := range s.records {
		if nameInZone(name, zone) {
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}
	for name := range s.dynamicRecords {
		if _, ok := seen[name]; !ok && nameInZone(name, zone) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		for _, ip := range s.records[name] {
			rrs = append(rrs, &dnsv2.A{
				Hdr: dnsv2.Header{Name: name, Class: dnsv2.ClassINET, TTL: s.ttl},
				A:   rdata.A{Addr: ip},
			})
		}
		types := make([]int, 0, len(s.dynamicRecords[name]))
		for rrType := range s.dynamicRecords[name] {
			types = append(types, int(rrType))
		}
		sort.Ints(types)
		for _, rawType := range types {
			rrType := uint16(rawType)
			rrs = append(rrs, dynamicRRs(name, rrType, s.dynamicRecords[name][rrType])...)
		}
	}

	rrs = append(rrs, s.soaRR(zone))
	return rrs
}

func (s *EmbeddedServer) zoneForNameLocked(name string) (string, bool) {
	for zone := range s.zones {
		if nameInZone(name, zone) {
			return zone, true
		}
	}
	return "", false
}

func (s *EmbeddedServer) soaRR(zone string) *dnsv2.SOA {
	serial := uint32(s.serialAt.Unix())
	if serial == 0 {
		serial = uint32(nowUTC().Unix())
	}
	return &dnsv2.SOA{
		Hdr: dnsv2.Header{Name: zone, Class: dnsv2.ClassINET, TTL: s.ttl},
		SOA: rdata.SOA{
			Ns:      "ns1." + zone,
			Mbox:    "hostmaster." + zone,
			Serial:  serial,
			Refresh: 3600,
			Retry:   600,
			Expire:  86400,
			Minttl:  s.ttl,
		},
	}
}

func (s *EmbeddedServer) nsRR(zone string) *dnsv2.NS {
	return &dnsv2.NS{
		Hdr: dnsv2.Header{Name: zone, Class: dnsv2.ClassINET, TTL: s.ttl},
		NS:  rdata.NS{Ns: "ns1." + zone},
	}
}

func (s *EmbeddedServer) loadDynamicRecordsLocked() error {
	if s.dynamicRecordsPath == "" {
		return nil
	}
	data, err := os.ReadFile(s.dynamicRecordsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read dynamic dns records: %w", err)
	}
	var file dynamicRecordFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse dynamic dns records: %w", err)
	}
	records := map[string]map[uint16]dynamicRecordSet{}
	for _, entry := range file.Records {
		name, ok := canonicalFQDN(entry.Name)
		if !ok {
			continue
		}
		rrType, ok := dynamicTypeFromName(entry.Type)
		if !ok {
			continue
		}
		ttl := entry.TTL
		if ttl == 0 {
			ttl = s.ttl
		}
		for _, value := range entry.Values {
			addDynamicRecordValue(records, name, rrType, ttl, value)
		}
	}
	s.dynamicRecords = records
	return nil
}

func (s *EmbeddedServer) persistDynamicRecordsLocked() error {
	if s.dynamicRecordsPath == "" {
		return nil
	}
	file := dynamicRecordFile{Records: dynamicRecordEntries(s.dynamicRecords)}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dynamic dns records: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.dynamicRecordsPath), 0o755); err != nil {
		return fmt.Errorf("create dynamic dns record dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.dynamicRecordsPath), ".records-*.json")
	if err != nil {
		return fmt.Errorf("create dynamic dns record temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write dynamic dns records: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close dynamic dns records: %w", err)
	}
	if err := os.Rename(tmpName, s.dynamicRecordsPath); err != nil {
		return fmt.Errorf("replace dynamic dns records: %w", err)
	}
	return nil
}

func dynamicRecordEntries(records map[string]map[uint16]dynamicRecordSet) []dynamicRecordEntry {
	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := []dynamicRecordEntry{}
	for _, name := range names {
		types := make([]int, 0, len(records[name]))
		for rrType := range records[name] {
			types = append(types, int(rrType))
		}
		sort.Ints(types)
		for _, rawType := range types {
			rrType := uint16(rawType)
			set := records[name][rrType]
			values := append([]string(nil), set.Values...)
			sort.Strings(values)
			entries = append(entries, dynamicRecordEntry{
				Name:   name,
				Type:   dynamicTypeName(rrType),
				TTL:    set.TTL,
				Values: values,
			})
		}
	}
	return entries
}

func dynamicRRs(name string, rrType uint16, set dynamicRecordSet) []dnsv2.RR {
	rrs := make([]dnsv2.RR, 0, len(set.Values))
	ttl := set.TTL
	for _, value := range set.Values {
		switch rrType {
		case dnsv2.TypeA:
			addr, err := netip.ParseAddr(value)
			if err != nil || !addr.Is4() {
				continue
			}
			rrs = append(rrs, &dnsv2.A{
				Hdr: dnsv2.Header{Name: name, Class: dnsv2.ClassINET, TTL: ttl},
				A:   rdata.A{Addr: addr},
			})
		case dnsv2.TypeTXT:
			rrs = append(rrs, &dnsv2.TXT{
				Hdr: dnsv2.Header{Name: name, Class: dnsv2.ClassINET, TTL: ttl},
				TXT: rdata.TXT{Txt: []string{value}},
			})
		case dnsv2.TypeCNAME:
			target, ok := canonicalFQDN(value)
			if !ok {
				continue
			}
			rrs = append(rrs, &dnsv2.CNAME{
				Hdr:   dnsv2.Header{Name: name, Class: dnsv2.ClassINET, TTL: ttl},
				CNAME: rdata.CNAME{Target: target},
			})
		}
	}
	return rrs
}

func dynamicRecordValue(rr dnsv2.RR) (string, bool) {
	switch typed := rr.(type) {
	case *dnsv2.A:
		if !typed.A.Addr.Is4() {
			return "", false
		}
		return typed.A.Addr.String(), true
	case *dnsv2.TXT:
		return strings.Join(typed.TXT.Txt, ""), true
	case *dnsv2.CNAME:
		target, ok := canonicalFQDN(typed.CNAME.Target)
		return target, ok
	default:
		return "", false
	}
}

func addDynamicRecordValue(records map[string]map[uint16]dynamicRecordSet, name string, rrType uint16, ttl uint32, value string) {
	if records[name] == nil {
		records[name] = map[uint16]dynamicRecordSet{}
	}
	set := records[name][rrType]
	if set.TTL == 0 {
		set.TTL = ttl
	}
	if rrType == dnsv2.TypeCNAME {
		set.Values = []string{value}
	} else if !containsString(set.Values, value) {
		set.Values = append(set.Values, value)
	}
	records[name][rrType] = set
}

func deleteDynamicRecordSet(records map[string]map[uint16]dynamicRecordSet, name string, rrType uint16) {
	if records[name] == nil {
		return
	}
	delete(records[name], rrType)
	if len(records[name]) == 0 {
		delete(records, name)
	}
}

func deleteDynamicRecordValue(records map[string]map[uint16]dynamicRecordSet, name string, rrType uint16, value string) {
	if records[name] == nil {
		return
	}
	set, ok := records[name][rrType]
	if !ok {
		return
	}
	values := set.Values[:0]
	for _, existing := range set.Values {
		if existing != value {
			values = append(values, existing)
		}
	}
	if len(values) == 0 {
		deleteDynamicRecordSet(records, name, rrType)
		return
	}
	set.Values = values
	records[name][rrType] = set
}

func updateZone(req *dnsv2.Msg) (string, bool) {
	if len(req.Question) != 1 {
		return "", false
	}
	zone, ok := canonicalFQDN(req.Question[0].Header().Name)
	if !ok {
		return "", false
	}
	return zone, true
}

func nameInZone(name, zone string) bool {
	return name == zone || strings.HasSuffix(name, "."+zone)
}

func dynamicTypeName(rrType uint16) string {
	switch rrType {
	case dnsv2.TypeA:
		return "A"
	case dnsv2.TypeTXT:
		return "TXT"
	case dnsv2.TypeCNAME:
		return "CNAME"
	default:
		return fmt.Sprintf("TYPE%d", rrType)
	}
}

func dynamicTypeFromName(name string) (uint16, bool) {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "A":
		return dnsv2.TypeA, true
	case "TXT":
		return dnsv2.TypeTXT, true
	case "CNAME":
		return dnsv2.TypeCNAME, true
	default:
		return 0, false
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func updateErrorCode(err error) uint16 {
	switch {
	case errors.Is(err, errDynamicNotZone):
		return dnsv2.RcodeNotZone
	case errors.Is(err, errDynamicUnsupported):
		return dnsv2.RcodeNotImplemented
	default:
		return dnsv2.RcodeFormatError
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

var (
	errDynamicFormat      = errors.New("dynamic dns update format error")
	errDynamicNotZone     = errors.New("dynamic dns update outside zone")
	errDynamicUnsupported = errors.New("dynamic dns record type unsupported")
)
