// Copyright 2026 Telos Authors. Licensed under the Apache License, Version 2.0.

package governor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/scttfrdmn/telos/acs"
)

// The ledger is WAL-backed so escrow/settle survive a crash (architecture §9).
// The WAL is an append-only file of one JSON record per line; each mutating
// operation (reserve/settle/release) is journaled BEFORE the in-memory state
// changes (write-ahead). On Open the log is replayed to reconstruct state. A
// torn final record (partial line from a crash mid-write) is tolerated and
// dropped — everything before it is conserved.
//
// Decided (pre-M2): append-only file + replay, no external dependency. The
// journal hook is isolated so a pluggable/compacting/ distributed WAL can replace
// it for M5 without touching the conservation logic.

type walOp string

const (
	opOpen    walOp = "open"    // root envelope established
	opReserve walOp = "reserve" // escrow a child against a parent
	opSettle  walOp = "settle"  // settle actual + bank surplus iff accepted
	opRelease walOp = "release" // release escrow with no charge
	opFault   walOp = "fault"   // a fault disposition for an entity/session (#C1)
)

// walRecord is one journaled mutation. Fields are populated per op; absent ones
// stay zero. Amounts are in the grant's currency.
type walRecord struct {
	Op walOp `json:"op"`

	// Open: the root envelope.
	Envelope *acs.Budget `json:"envelope,omitempty"`

	// Reserve: a child id under Parent for Amount over Period.
	ID     GrantID       `json:"id,omitempty"`
	Parent GrantID       `json:"parent,omitempty"`
	Amount float64       `json:"amount,omitempty"`
	Period time.Duration `json:"period,omitempty"`

	// Settle: actual cost (Amount above) + its modeled portion + the disposition
	// (acceptance/exit/cause) so replay reconstructs the surplus gate's RESULT and
	// the surplus signal (#D1) — never re-evaluating the gate unconditionally.
	Synthesized float64  `json:"synthesized,omitempty"`
	Accepted    bool     `json:"accepted,omitempty"`
	Exit        ExitKind `json:"exit,omitempty"`
	Cause       string   `json:"cause,omitempty"`

	// Fault (opFault): a session/entity fault disposition, journaled so a fault
	// recorded before a crash is reproduced legibly on replay (#C1).
	FaultClass string `json:"fault_class,omitempty"`
	FaultCode  string `json:"fault_code,omitempty"`
	FaultMsg   string `json:"fault_msg,omitempty"`
}

// wal is the append-only journal handle.
type wal struct {
	f   *os.File
	enc *json.Encoder
}

// journal appends a record to the WAL before the in-memory mutation. A nil WAL
// (in-memory-only governor) is a no-op. Caller holds m.mu.
func (m *Mem) journal(rec walRecord) error {
	if m.wal == nil {
		return nil
	}
	if err := m.wal.enc.Encode(rec); err != nil {
		return fmt.Errorf("governor: wal append: %w", err)
	}
	// Durability: flush to the OS and fsync so the write-ahead guarantee holds
	// across a process kill. (M2 favors correctness over throughput; batching is
	// a later optimization.)
	return m.wal.f.Sync()
}

// Open creates or reopens a WAL-backed governor at path. If the file exists, it
// is replayed to reconstruct grant state (including which grants are closed and
// what surplus they banked); the replayed envelope is used and the `envelope`
// argument is ignored. If the file is new, the envelope establishes the root
// grant and is journaled as the first record.
//
// Replay tolerates a torn final record (a partial line from a crash): it stops
// at the first undecodable trailing line, conserving everything written before.
func Open(path string, envelope acs.Budget) (*Mem, error) {
	existed := fileNonEmpty(path)

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("governor: open wal %s: %w", path, err)
	}

	m := &Mem{grants: make(map[GrantID]*grantState)}

	if existed {
		if err := m.replay(f); err != nil {
			f.Close()
			return nil, err
		}
		// Position the writer at end for subsequent appends.
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			f.Close()
			return nil, fmt.Errorf("governor: wal seek: %w", err)
		}
		m.wal = &wal{f: f, enc: json.NewEncoder(f)}
	} else {
		if err := envelope.Validate(); err != nil {
			f.Close()
			return nil, fmt.Errorf("governor: wal envelope: %w", err)
		}
		m.wal = &wal{f: f, enc: json.NewEncoder(f)}
		m.grants[RootGrant] = &grantState{id: RootGrant, parent: RootGrant, reservoir: envelope}
		if err := m.journal(walRecord{Op: opOpen, Envelope: &envelope}); err != nil {
			f.Close()
			return nil, err
		}
	}
	return m, nil
}

// Close flushes and closes the WAL. The in-memory state remains usable but
// un-journaled afterward.
func (m *Mem) Close() error {
	if m.wal == nil {
		return nil
	}
	err := m.wal.f.Close()
	m.wal = nil
	return err
}

// replay reconstructs state from the journal. It applies each well-formed record
// via the same *Locked helpers the live path uses (with wal=false so replay is
// not re-journaled), and stops at the first torn/undecodable trailing record.
func (m *Mem) replay(f *os.File) error {
	// Replay reconstructs state; suppress live surplus-signal pushes during it
	// (re-emitting historical signals would double-count realized burn — #D1).
	m.replaying = true
	defer func() { m.replaying = false }()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("governor: wal rewind: %w", err)
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		var rec walRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			// Torn final record from a crash mid-append: stop, conserving prior
			// records. (Only acceptable on the LAST line; a mid-file corruption
			// would surface as a downstream apply error below.)
			break
		}
		if err := m.applyRecord(rec); err != nil {
			return fmt.Errorf("governor: wal replay line %d: %w", line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("governor: wal scan: %w", err)
	}
	return nil
}

// applyRecord replays one journaled mutation onto in-memory state (wal=false).
func (m *Mem) applyRecord(rec walRecord) error {
	switch rec.Op {
	case opOpen:
		if rec.Envelope == nil {
			return fmt.Errorf("open record missing envelope")
		}
		m.grants[RootGrant] = &grantState{id: RootGrant, parent: RootGrant, reservoir: *rec.Envelope}
		return nil
	case opReserve:
		if _, ok := m.grants[rec.Parent]; !ok {
			return fmt.Errorf("reserve parent %q absent", rec.Parent)
		}
		m.applyReserve(rec.ID, rec.Parent, acs.BudgetRequest{Amount: rec.Amount, Period: rec.Period})
		m.bumpNextID(rec.ID)
		return nil
	case opSettle:
		g, ok := m.grants[rec.ID]
		if !ok {
			return fmt.Errorf("settle for absent grant %q", rec.ID)
		}
		cur := g.reservoir.Denomination()
		// Replay reconstructs the disposition (Accepted/Exit/Cause) recorded at
		// settle time — the surplus gate's RESULT, never re-evaluated. settleLocked
		// is idempotent, so replaying an applied settle is a no-op.
		return m.settleLocked(rec.ID, acs.Cost{Amount: rec.Amount, Synthesized: rec.Synthesized, Currency: cur},
			Outcome{Accepted: rec.Accepted, Exit: rec.Exit, Cause: rec.Cause}, false)
	case opRelease:
		return m.releaseLocked(rec.ID, false)
	case opFault:
		return m.applyFaultLocked(rec, false)
	default:
		return fmt.Errorf("unknown wal op %q", rec.Op)
	}
}

// bumpNextID ensures freshly minted ids never collide with replayed ones.
func (m *Mem) bumpNextID(id GrantID) {
	var n uint64
	if _, err := fmt.Sscanf(string(id), "g%d", &n); err == nil && n > m.nextID {
		m.nextID = n
	}
}

func fileNonEmpty(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}
