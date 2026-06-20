// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// ComputeHash computes the Spec's content hash and stores it in Hash, returning
// the value. The Hash field is excluded from its own digest input so the hash is
// stable and self-consistent (a Spec's hash never depends on a previously
// computed hash). The digest is SHA-256 over a canonical JSON encoding.
//
// This is the content-address used for versioned re-planning: re-plan can be a
// diff against a prior Hash (architecture §15 #10).
func (s *Spec) ComputeHash() (string, error) {
	h, err := s.contentHash()
	if err != nil {
		return "", err
	}
	s.Hash = h
	return h, nil
}

// VerifyHash recomputes the content hash and reports whether it matches the
// stored Hash. A Spec with an empty Hash is treated as unverified (error).
func (s *Spec) VerifyHash() error {
	if s.Hash == "" {
		return fmt.Errorf("spec has no hash to verify")
	}
	want := s.Hash
	got, err := s.contentHash()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("spec hash mismatch: stored %s, computed %s", want, got)
	}
	return nil
}

// contentHash digests the Spec with Hash zeroed, over canonical JSON.
func (s *Spec) contentHash() (string, error) {
	clone := *s
	clone.Hash = ""
	canon, err := canonicalJSON(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canon)
	return fmt.Sprintf("sha256:%x", sum), nil
}

// canonicalJSON produces a deterministic JSON encoding: Go's encoding/json sorts
// map keys, and we re-encode through a generic decode to sort object keys in any
// nested structures consistently. Slices preserve order (it is semantic for
// Children/Edges). This is sufficient for a stable content-address.
func canonicalJSON(v any) ([]byte, error) {
	// First pass: marshal with stdlib (map keys already sorted by encoding/json).
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Second pass: decode to generic and re-marshal so any map[string]any nested
	// values are also key-sorted deterministically by canonicalize.
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return marshalCanonical(generic)
}

// marshalCanonical encodes a decoded-JSON value with object keys sorted.
func marshalCanonical(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf := []byte{'{'}
		for i, k := range keys {
			if i > 0 {
				buf = append(buf, ',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			buf = append(buf, kb...)
			buf = append(buf, ':')
			vb, err := marshalCanonical(t[k])
			if err != nil {
				return nil, err
			}
			buf = append(buf, vb...)
		}
		return append(buf, '}'), nil
	case []any:
		buf := []byte{'['}
		for i, e := range t {
			if i > 0 {
				buf = append(buf, ',')
			}
			eb, err := marshalCanonical(e)
			if err != nil {
				return nil, err
			}
			buf = append(buf, eb...)
		}
		return append(buf, ']'), nil
	default:
		// Scalars (string, float64, bool, nil) marshal deterministically.
		return json.Marshal(t)
	}
}
