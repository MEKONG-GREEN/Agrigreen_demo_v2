package store

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
)

const archiveProjectionEntries = 16
const archiveProjectionBytes = 1 << 20
const archiveProjectionFixedBytes = 4096
const archiveProjectionEntryBytes = 256

type archiveProjectionKey struct{ payload, filter string }
type archiveProjectionEntry struct {
	fields domain.ArchiveFields
	bytes  int
}

// A memo belongs to exactly one SQL walk, after current-reader/RLS checks. It
// retains pure frozen metadata only; row identity and authority are never cached.
type archiveProjectionMemo struct {
	entries            map[archiveProjectionKey]archiveProjectionEntry
	order              [archiveProjectionEntries]archiveProjectionKey
	next, count, bytes int
}

func cloneArchiveFields(in domain.ArchiveFields) domain.ArchiveFields {
	if in.Codes != nil {
		codes := make([]string, len(in.Codes))
		copy(codes, in.Codes)
		in.Codes = codes
	}
	return in
}

func archiveProjectionSize(payload, filter []byte, fields domain.ArchiveFields) int {
	size := len(payload) + len(filter) + archiveProjectionEntryBytes + len(fields.Status) + len(fields.Codes)*16
	for _, code := range fields.Codes {
		size += len(code)
	}
	return size
}

func (m *archiveProjectionMemo) project(payload, filter []byte) domain.ArchiveFields {
	// Oversized keys bypass before any retained key copy. They still receive the
	// normal projection, including unknown/row-count semantics.
	if len(payload)+len(filter)+archiveProjectionEntryBytes > archiveProjectionBytes-archiveProjectionFixedBytes {
		return domain.ProjectArchiveFields(payload, filter)
	}
	key := archiveProjectionKey{payload: string(payload), filter: string(filter)}
	if entry, ok := m.entries[key]; ok {
		return cloneArchiveFields(entry.fields)
	}
	fields := domain.ProjectArchiveFields(payload, filter)
	size := archiveProjectionSize(payload, filter, fields)
	if size > archiveProjectionBytes-archiveProjectionFixedBytes {
		return fields
	}
	if m.entries == nil {
		m.entries = make(map[archiveProjectionKey]archiveProjectionEntry, archiveProjectionEntries)
	}
	for m.count > 0 && (m.count == archiveProjectionEntries || m.bytes+size > archiveProjectionBytes-archiveProjectionFixedBytes) {
		oldest := m.order[m.next]
		m.bytes -= m.entries[oldest].bytes
		delete(m.entries, oldest)
		m.order[m.next] = archiveProjectionKey{}
		m.next = (m.next + 1) % archiveProjectionEntries
		m.count--
	}
	index := (m.next + m.count) % archiveProjectionEntries
	m.order[index] = key
	m.entries[key] = archiveProjectionEntry{fields: cloneArchiveFields(fields), bytes: size}
	m.bytes += size
	m.count++
	return fields
}
