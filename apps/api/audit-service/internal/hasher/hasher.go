// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package hasher computes the per-row sha256 chain hash for the F031 audit
// ledger.
//
// Canonical JSON (RFC 8785 / JCS-lite) rules implemented here:
//
//   - Object keys are sorted lexicographically by their UTF-8 byte sequence.
//   - No insignificant whitespace anywhere.
//   - Strings are emitted with the smallest escape set: control characters,
//     quote, backslash. Unicode passes through unchanged.
//   - Numbers are emitted via strconv with the shortest round-trippable form.
//
// We do not depend on github.com/gibson042/canonicaljson-go — keeping the
// implementation in-tree means the hash function is byte-stable across Go
// toolchain upgrades and we can audit every line of the canonicalizer.
//
// The chain rule:
//
//	entry_hash = sha256(
//	    canonical_json({
//	        "tenant_id":  tenant_id,
//	        "id":         id,
//	        "actor":      actor,
//	        "action":     action,
//	        "resource":   resource,
//	        "payload":    payload,
//	        "prev_hash":  base64(prev_hash),
//	        "created_at": created_at,
//	    })
//	)
//
// `prev_hash` is encoded as standard base64 (with padding) so the canonical
// JSON shape stays compact and re-computable in any language.
package hasher

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

// HashSize is the length in bytes of every prev_hash / entry_hash slice.
const HashSize = sha256.Size

// Entry is the minimum tuple the hasher needs. Field names match the on-disk
// column names so callers can construct it directly from a row scan.
type Entry struct {
	TenantID  string
	ID        int64
	Actor     map[string]any
	Action    string
	Resource  map[string]any
	Payload   map[string]any
	PrevHash  []byte
	CreatedAt time.Time
}

// Compute returns sha256 over the canonical JSON encoding of e.
func Compute(e Entry) ([]byte, error) {
	body, err := Canonical(e)
	if err != nil {
		return nil, fmt.Errorf("hasher: canonicalize: %w", err)
	}
	sum := sha256.Sum256(body)
	out := make([]byte, HashSize)
	copy(out, sum[:])
	return out, nil
}

// Canonical returns the canonical JSON bytes the hash is computed over.
// Exposed so the verify endpoint can show the exact input on mismatch.
func Canonical(e Entry) ([]byte, error) {
	if len(e.PrevHash) != HashSize {
		return nil, fmt.Errorf("hasher: prev_hash must be %d bytes, got %d", HashSize, len(e.PrevHash))
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
	if err := encode(&sb, doc); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

// sanitizeMap returns a copy of m with stable shape: nil maps become empty
// maps so the canonical form is {} rather than null. Callers may safely
// pass nil for "no payload".
func sanitizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// encode writes the canonical JSON encoding of v into w.
func encode(w *strings.Builder, v any) error {
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
		encodeString(w, x)
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
			if err := encode(w, el); err != nil {
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
			encodeString(w, k)
			w.WriteByte(':')
			if err := encode(w, x[k]); err != nil {
				return err
			}
		}
		w.WriteByte('}')
	default:
		// Fall back to encoding/json for any type we did not enumerate
		// (e.g. anonymous structs). The fallback uses encoding/json's
		// default ordering which is field-declaration order for structs —
		// callers must not rely on that path for hash stability and are
		// expected to feed map[string]any / []any only.
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Errorf("hasher: unsupported type %T: %w", v, err)
		}
		w.Write(b)
	}
	return nil
}

func encodeString(w *strings.Builder, s string) {
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
