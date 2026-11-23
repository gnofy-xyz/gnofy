package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const LockfileVersion = "1"

// LockEntry representa una resolución verificada y persistida en gnofy.lock.
type LockEntry struct {
	HoleID   string `json:"hole_id"`
	Reason   string `json:"reason"`
	Scope    string `json:"scope,omitempty"`
	Model    string `json:"model"`
	GnoCode  string `json:"gno_code"`
	Verified bool   `json:"verified"`
}

// Lockfile fija resoluciones asistidas por IA para mantener builds reproducibles en CI.
type Lockfile struct {
	Version string               `json:"version"`
	Entries map[string]LockEntry `json:"entries"`
}

// NewLockfile inicializa un Lockfile vacío listo para usar.
func NewLockfile() *Lockfile {
	return &Lockfile{
		Version: LockfileVersion,
		Entries: make(map[string]LockEntry),
	}
}

// LoadLockfile carga gnofy.lock si existe, o devuelve uno vacío si aún no existe.
func LoadLockfile(path string) (*Lockfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewLockfile(), nil
		}
		return nil, fmt.Errorf("leyendo lockfile %s: %w", path, err)
	}
	var lf Lockfile
	if err := json.Unmarshal(raw, &lf); err != nil {
		return nil, fmt.Errorf("parseando lockfile %s: %w", path, err)
	}
	if lf.Version == "" {
		lf.Version = LockfileVersion
	}
	if lf.Entries == nil {
		lf.Entries = make(map[string]LockEntry)
	}
	return &lf, nil
}

// SaveLockfile escribe el lockfile en JSON determinista (claves ordenadas por encoding/json).
func SaveLockfile(path string, lf *Lockfile) error {
	if lf == nil {
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir lockfile dir %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("serializando lockfile: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// ResolveStats contabiliza la procedencia exacta de cada hueco durante una conversión.
type ResolveStats struct {
	TotalHoles  int
	FromLock    int
	FromAI      int
	FromStub    int
	PendingLock map[string]LockEntry
}

// CachedResolver envuelve un HoleResolver con lectura de gnofy.lock y registro de estadísticas.
type CachedResolver struct {
	Inner     HoleResolver
	Lock      *Lockfile
	BypassIDs map[string]bool // huecos a re-resolver en un reintento de reparación
	PrevError string          // error de compilación gno a inyectar en reintentos
	Stats     ResolveStats
}

// NewCachedResolver crea un CachedResolver sobre lock e inner.
func NewCachedResolver(inner HoleResolver, lock *Lockfile) *CachedResolver {
	if inner == nil {
		inner = NoopResolver{}
	}
	if lock == nil {
		lock = NewLockfile()
	}
	return &CachedResolver{
		Inner: inner,
		Lock:  lock,
		Stats: ResolveStats{
			PendingLock: make(map[string]LockEntry),
		},
	}
}

// ResetAttempt reinicia contadores antes de un reintento de reparación.
func (c *CachedResolver) ResetAttempt(prevErr string, bypassIDs map[string]bool) {
	c.PrevError = prevErr
	c.BypassIDs = bypassIDs
	c.Stats = ResolveStats{
		PendingLock: make(map[string]LockEntry),
	}
}

// Resolve consulta primero gnofy.lock (si verificado y no en BypassIDs) y si no invoca Inner.
func (c *CachedResolver) Resolve(ctx context.Context, req HoleRequest) (HoleResolution, error) {
	c.Stats.TotalHoles++

	if req.Hole != nil && !c.BypassIDs[req.Hole.ID] {
		if entry, ok := c.Lock.Entries[req.Hole.ID]; ok && entry.Verified && entry.GnoCode != "" {
			c.Stats.FromLock++
			return HoleResolution{
				GnoCode: entry.GnoCode,
				Model:   entry.Model,
				Source:  SourceLock,
				Cached:  true,
			}, nil
		}
	}

	if c.PrevError != "" && req.PreviousError == "" {
		req.PreviousError = c.PrevError
	}

	res, err := c.Inner.Resolve(ctx, req)
	if err != nil {
		// Fallback seguro a stub determinista si falla el cliente IA
		c.Stats.FromStub++
		stub, _ := (NoopResolver{}).Resolve(ctx, req)
		return stub, err
	}

	switch res.Source {
	case SourceStub:
		c.Stats.FromStub++
	case SourceLock:
		c.Stats.FromLock++
	default:
		res.Source = SourceAIFresh
		c.Stats.FromAI++
		if req.Hole != nil {
			c.Stats.PendingLock[req.Hole.ID] = LockEntry{
				HoleID:   req.Hole.ID,
				Reason:   req.Hole.Reason,
				Scope:    req.Hole.ScopeSummary,
				Model:    res.Model,
				GnoCode:  res.GnoCode,
				Verified: false,
			}
		}
	}

	return res, nil
}

// CommitVerified marca todas las resoluciones frescas pendientes como verificadas y las guarda en Lock.
func (c *CachedResolver) CommitVerified() {
	for id, entry := range c.Stats.PendingLock {
		entry.Verified = true
		c.Lock.Entries[id] = entry
	}
}
