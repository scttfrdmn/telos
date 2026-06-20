// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package acs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Load parses an ACS from JSON bytes, validates it, and verifies its content
// hash if one is present. A spec that fails validation is returned as nil with
// the error — callers never receive an invalid Spec.
//
// If the file carries a Hash, it must match the computed content hash; this
// catches a hand-edited seed whose hash was not regenerated. A file with no Hash
// is accepted (the seed may be authored without one and hashed on load).
func Load(data []byte) (*Spec, error) {
	var s Spec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // a typo'd field in a hand-authored seed is an error, not silence
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("acs: decode: %w", err)
	}

	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("acs: invalid spec: %w", err)
	}

	if s.Hash != "" {
		if err := s.VerifyHash(); err != nil {
			return nil, fmt.Errorf("acs: %w", err)
		}
	} else {
		if _, err := s.ComputeHash(); err != nil {
			return nil, fmt.Errorf("acs: hash: %w", err)
		}
	}
	return &s, nil
}

// LoadFile reads and Loads an ACS from a path.
func LoadFile(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("acs: read %s: %w", path, err)
	}
	s, err := Load(data)
	if err != nil {
		return nil, fmt.Errorf("acs: %s: %w", path, err)
	}
	return s, nil
}

// Marshal serializes a Spec to indented JSON, computing its hash first so the
// emitted file is self-verifying.
func (s *Spec) Marshal() ([]byte, error) {
	if _, err := s.ComputeHash(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(s, "", "  ")
}
