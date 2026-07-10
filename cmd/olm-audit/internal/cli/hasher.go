// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package cli

// This file is an exact byte-for-byte copy of the canonical-JSON + sha256
// chain helper that the audit-service runs on insert
// (apps/api/audit-service/internal/hasher/hasher.go). The duplication is
// deliberate: the olm-audit CLI is the auditor's trust artifact, so it
// must compute the chain hash without depending on any of the live
// service's internal packages.
//
// If you edit this file you MUST mirror the change in
// apps/api/audit-service/internal/hasher/hasher.go, and vice versa. A
// drift between the two implementations is the only failure mode that
// silently produces false BREAK reports.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// hashSize is the length in bytes of every prev_hash / entry_hash slice.
const hashSize = sha256.Size

// hasherEntry is the minimum tuple the hasher needs.
type hasherEntry struct {
	TenantID  string
	ID        int64
	Actor     map[string]any
	Action    string
	Resource  map[string]any
	Payload   map[string]any
	PrevHash  []byte
	CreatedAt time.Time
}

// computeHash returns sha256 over the canonical JSON encoding of e.
func computeHash(e hasherEntry) ([]byte, error) {
	if len(e.PrevHash) != hashSize {
		return nil, fmt.Errorf("hasher: prev_hash must be %d bytes, got %d", hashSize, len(e.PrevHash))
	}
	doc := map[string]any{
		"tenant_id":  e.TenantID,
		"id":         e.ID,
		"actor":      sanitizeMap(e.Actor),
		"action":     e.Action,
		"resource":   sanitizeMap(e.Resource),
		"payload":    sanitizeMap(e.Payload),
		"prev_hash":  base64.StdEncoding.EncodeToString(e.PrevHash),
		"created_at": e.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	var sb strings.Builder
	if err := encodeValue(&sb, doc); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(sb.String()))
	out := make([]byte, hashSize)
	copy(out, sum[:])
	return out, nil
}

func sanitizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func encodeValue(w *strings.Builder, v any) error {
	switch x := v.(type) {
	case nil:
		w.WriteString("null")
	case bool:
		if x {
			w.WriteString("true")
		} else {
			w.WriteString("false")
		}
	case string:
		encodeStr(w, x)
	case int:
		w.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		w.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		w.WriteString(strconv.FormatInt(x, 10))
	case uint:
		w.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint32:
		w.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		w.WriteString(strconv.FormatUint(x, 10))
	case float32:
		w.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		w.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case json.Number:
		w.WriteString(string(x))
	case []any:
		w.WriteByte('[')
		for i, el := range x {
			if i > 0 {
				w.WriteByte(',')
			}
			if err := encodeValue(w, el); err != nil {
				return err
			}
		}
		w.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				w.WriteByte(',')
			}
			encodeStr(w, k)
			w.WriteByte(':')
			if err := encodeValue(w, x[k]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Errorf("hasher: unsupported type %T: %w", v, err)
		}
		w.Write(b)
	}
	return nil
}

func encodeStr(w *strings.Builder, s string) {
	w.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			w.WriteString(`\"`)
		case '\\':
			w.WriteString(`\\`)
		case '\n':
			w.WriteString(`\n`)
		case '\r':
			w.WriteString(`\r`)
		case '\t':
			w.WriteString(`\t`)
		case '\b':
			w.WriteString(`\b`)
		case '\f':
			w.WriteString(`\f`)
		default:
			if r < 0x20 {
				w.WriteString(`\u`)
				fmt.Fprintf(w, "%04x", r)
			} else {
				w.WriteRune(r)
			}
		}
	}
	w.WriteByte('"')
}
