package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/rdata"
	"net/netip"
	"sort"
	"strings"
)

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
				Name:      name,
				Type:      dynamicTypeName(rrType),
				TTL:       set.TTL,
				Values:    values,
				CreatedAt: set.CreatedAt,
				UpdatedAt: set.UpdatedAt,
			})
		}
	}
	return entries
}

func dynamicRecordsFromMap(records map[string]map[uint16]dynamicRecordSet) []DynamicRecord {
	entries := dynamicRecordEntries(records)
	out := make([]DynamicRecord, 0, len(entries))
	for _, entry := range entries {
		rrType, ok := dynamicTypeFromName(entry.Type)
		if !ok {
			continue
		}
		out = append(out, DynamicRecord{
			Name:      entry.Name,
			Type:      dynamicTypeName(rrType),
			TTL:       entry.TTL,
			Values:    append([]string(nil), entry.Values...),
			CreatedAt: entry.CreatedAt,
			UpdatedAt: entry.UpdatedAt,
		})
	}
	return out
}

func dynamicRecordFromSet(name string, rrType uint16, set dynamicRecordSet) DynamicRecord {
	values := append([]string(nil), set.Values...)
	sort.Strings(values)
	return DynamicRecord{
		Name:      name,
		Type:      dynamicTypeName(rrType),
		TTL:       set.TTL,
		Values:    values,
		CreatedAt: set.CreatedAt,
		UpdatedAt: set.UpdatedAt,
	}
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
	now := nowUTC()
	set := records[name][rrType]
	if set.TTL == 0 {
		set.TTL = ttl
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = now
	}
	set.UpdatedAt = now
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
	set.UpdatedAt = nowUTC()
	records[name][rrType] = set
}

func normalizeDynamicRecordValues(rrType uint16, raw []string) []string {
	values := []string{}
	seen := map[string]struct{}{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if rrType == dnsv2.TypeCNAME {
			if target, ok := canonicalFQDN(value); ok {
				value = target
			}
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func validDynamicRecordValue(rrType uint16, value string) bool {
	switch rrType {
	case dnsv2.TypeA:
		addr, err := netip.ParseAddr(value)
		return err == nil && addr.Is4()
	case dnsv2.TypeTXT:
		return strings.TrimSpace(value) != ""
	case dnsv2.TypeCNAME:
		if hasDNSNameUnsafeChars(value) {
			return false
		}
		_, ok := canonicalFQDN(value)
		return ok
	default:
		return false
	}
}

func hasDNSNameUnsafeChars(value string) bool {
	return strings.ContainsAny(strings.TrimSpace(value), " \t\r\n/")
}
