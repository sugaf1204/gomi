package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"log"
)

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
