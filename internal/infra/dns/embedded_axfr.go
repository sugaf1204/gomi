package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"codeberg.org/miekg/dns/rdata"
	"context"
	"log"
	"sort"
)

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
