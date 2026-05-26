package dns

import (
	dnsv2 "codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/rdata"
	"context"
	"errors"
	"time"
)

type dynamicRecordSet struct {
	TTL       uint32
	Values    []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type dynamicRecordFile struct {
	Records []dynamicRecordEntry `json:"records"`
}

type dynamicRecordEntry struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	TTL       uint32    `json:"ttl"`
	Values    []string  `json:"values"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type DynamicRecord struct {
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	TTL       uint32    `json:"ttl"`
	Values    []string  `json:"values"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *EmbeddedServer) ListDynamicRecords(_ context.Context) ([]DynamicRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDynamicRecordsLoadedLocked(); err != nil {
		return nil, err
	}
	return dynamicRecordsFromMap(s.dynamicRecords), nil
}

func (s *EmbeddedServer) UpsertDynamicRecord(_ context.Context, record DynamicRecord) (DynamicRecord, error) {
	name, rrType, ttl, values, err := s.validateDynamicRecordShape(record)
	if err != nil {
		return DynamicRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDynamicRecordsLoadedLocked(); err != nil {
		return DynamicRecord{}, err
	}
	if err := s.validateDynamicRecordPlacementLocked(name, rrType); err != nil {
		return DynamicRecord{}, err
	}

	now := nowUTC()
	createdAt := now
	if existing, ok := s.dynamicRecords[name][rrType]; ok && !existing.CreatedAt.IsZero() {
		createdAt = existing.CreatedAt
	}
	if s.dynamicRecords[name] == nil {
		s.dynamicRecords[name] = map[uint16]dynamicRecordSet{}
	}
	s.dynamicRecords[name][rrType] = dynamicRecordSet{
		TTL:       ttl,
		Values:    values,
		CreatedAt: createdAt,
		UpdatedAt: now,
	}
	if err := s.persistDynamicRecordsLocked(); err != nil {
		return DynamicRecord{}, err
	}
	s.serialAt = now
	return dynamicRecordFromSet(name, rrType, s.dynamicRecords[name][rrType]), nil
}

func (s *EmbeddedServer) DeleteDynamicRecord(_ context.Context, rawName, rawType string) error {
	name, ok := canonicalFQDN(rawName)
	if !ok {
		return errDynamicFormat
	}
	rrType, ok := dynamicTypeFromName(rawType)
	if !ok {
		return errDynamicUnsupported
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDynamicRecordsLoadedLocked(); err != nil {
		return err
	}
	deleteDynamicRecordSet(s.dynamicRecords, name, rrType)
	if err := s.persistDynamicRecordsLocked(); err != nil {
		return err
	}
	s.serialAt = nowUTC()
	return nil
}

func (s *EmbeddedServer) validateDynamicRecordShape(record DynamicRecord) (string, uint16, uint32, []string, error) {
	if hasDNSNameUnsafeChars(record.Name) {
		return "", 0, 0, nil, errDynamicFormat
	}
	name, ok := canonicalFQDN(record.Name)
	if !ok {
		return "", 0, 0, nil, errDynamicFormat
	}
	rrType, ok := dynamicTypeFromName(record.Type)
	if !ok {
		return "", 0, 0, nil, errDynamicUnsupported
	}
	ttl := record.TTL
	if ttl == 0 {
		ttl = s.ttl
	}
	if ttl == 0 {
		return "", 0, 0, nil, errDynamicFormat
	}
	values := normalizeDynamicRecordValues(rrType, record.Values)
	if len(values) == 0 {
		return "", 0, 0, nil, errDynamicFormat
	}
	if rrType == dnsv2.TypeCNAME && len(values) != 1 {
		return "", 0, 0, nil, errDynamicFormat
	}
	for _, value := range values {
		if !validDynamicRecordValue(rrType, value) {
			return "", 0, 0, nil, errDynamicUnsupported
		}
	}
	return name, rrType, ttl, values, nil
}

func (s *EmbeddedServer) validateDynamicRecordPlacementLocked(name string, rrType uint16) error {
	zone, ok := s.zoneForNameLocked(name)
	if !ok || !nameInZone(name, zone) {
		return errDynamicNotZone
	}
	if rrType == dnsv2.TypeCNAME {
		if len(s.records[name]) > 0 {
			return errDynamicCNAMEConflict
		}
		for existingType := range s.dynamicRecords[name] {
			if existingType != dnsv2.TypeCNAME {
				return errDynamicCNAMEConflict
			}
		}
		return nil
	}
	if _, ok := s.dynamicRecords[name][dnsv2.TypeCNAME]; ok {
		return errDynamicCNAMEConflict
	}
	return nil
}

func IsDynamicRecordValidationError(err error) bool {
	return errors.Is(err, errDynamicFormat) ||
		errors.Is(err, errDynamicNotZone) ||
		errors.Is(err, errDynamicUnsupported) ||
		errors.Is(err, errDynamicCNAMEConflict)
}

func (s *EmbeddedServer) ensureDynamicRecordsLoadedLocked() error {
	if s.dynamicLoaded {
		return nil
	}
	if err := s.loadDynamicRecordsLocked(); err != nil {
		return err
	}
	s.dynamicLoaded = true
	return nil
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
