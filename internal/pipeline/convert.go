// Package pipeline orquesta el flujo completo de conversión Solidity -> Gno,
// incluyendo resolución opcional de huecos IA con gnofy.lock y bucle acotado
// de autorreparación verificado por `gno test`.
package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gnofy-xyz/gnofy/internal/ai"
	gnogen "github.com/gnofy-xyz/gnofy/internal/codegen/gno"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
	"github.com/gnofy-xyz/gnofy/internal/ir"
	"github.com/gnofy-xyz/gnofy/internal/passes"
	"github.com/gnofy-xyz/gnofy/internal/verify"
)

// Verifier abstrae la verificación con `gno test` para facilitar tests unitarios del bucle de reparación.
type Verifier interface {
	VerifyDir(ctx context.Context, dir string) (*verify.Result, error)
}

// ConvertOptions configura el pipeline completo de conversión y verificación.
type ConvertOptions struct {
	Inputs       []string
	OutDir       string
	Solc         solc.Options
	Codegen      gnogen.Options
	AIMode       string // "off" (default) o "assist"
	AIResolver   ai.HoleResolver
	LockfilePath string // ej. "gnofy.lock"
	Verify       bool
	Verifier     Verifier
	MaxRetries   int // reintentos máximos de reparación IA ante error de gno test (default 2)
}

// ConvertResult resume los artefactos generados y la procedencia exacta de cada hueco.
type ConvertResult struct {
	Files           []gnogen.OutputFile
	TotalHoles      int
	FromLock        int
	FromAI          int
	FromStub        int
	Attempts        int
	Verified        bool
	FellBackToStubs bool
	VerifyOutput    string
	LastError       string
}

// Convert ejecuta el pipeline completo: solc -> IR -> passes -> codegen (+ AI/lock) -> verify (+ repair loop).
func Convert(ctx context.Context, opts ConvertOptions) (*ConvertResult, error) {
	runner := solc.NewRunner(opts.Solc)
	compiled, err := runner.Compile(ctx, opts.Inputs)
	if err != nil {
		return nil, err
	}

	mod := ir.Lower(compiled)
	pipe := passes.DefaultPipeline()
	if opts.Codegen.Idiomatic {
		pipe = passes.IdiomaticPipeline()
	}
	if err := pipe.Run(mod); err != nil {
		return nil, err
	}

	return ConvertModule(ctx, mod, opts)
}

// ConvertModule ejecuta generación, caché gnofy.lock y bucle de verificación/reparación sobre una IR ya bajada.
func ConvertModule(ctx context.Context, mod *ir.Module, opts ConvertOptions) (*ConvertResult, error) {
	maxRetries := opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	aiActive := opts.AIMode == "assist"
	var lf *ai.Lockfile
	if aiActive && opts.LockfilePath != "" {
		var err error
		lf, err = ai.LoadLockfile(opts.LockfilePath)
		if err != nil {
			return nil, err
		}
	} else {
		lf = ai.NewLockfile()
	}

	inner := opts.AIResolver
	if !aiActive || inner == nil {
		inner = ai.NoopResolver{}
	}
	cached := ai.NewCachedResolver(inner, lf)

	genOpts := opts.Codegen
	genOpts.Resolver = cached

	var files []gnogen.OutputFile
	var verifyOut string
	var lastErr string
	attempts := 0

	shouldVerify := opts.Verify || (aiActive && opts.Verifier != nil)
	v := opts.Verifier
	if shouldVerify && v == nil {
		v = &verify.Verifier{}
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		attempts = attempt + 1
		gen := gnogen.NewGenerator(genOpts)
		var err error
		files, err = gen.GenerateContext(ctx, mod)
		if err != nil {
			return nil, err
		}
		if opts.OutDir != "" {
			if err := gnogen.WriteFiles(opts.OutDir, files); err != nil {
				return nil, err
			}
		}

		if !shouldVerify || opts.OutDir == "" {
			// Sin verificador activo: si hay resoluciones frescas sin verificar, no las marcamos Verified en lockfile
			return &ConvertResult{
				Files:      files,
				TotalHoles: cached.Stats.TotalHoles,
				FromLock:   cached.Stats.FromLock,
				FromAI:     cached.Stats.FromAI,
				FromStub:   cached.Stats.FromStub,
				Attempts:   attempts,
				Verified:   false,
			}, nil
		}

		pkgDirs := uniquePackageDirs(opts.OutDir, files)
		vOut, vErr := verifyAll(ctx, v, pkgDirs)
		verifyOut = vOut
		if vErr == nil {
			if aiActive && len(cached.Stats.PendingLock) > 0 {
				cached.CommitVerified()
				if opts.LockfilePath != "" {
					if err := ai.SaveLockfile(opts.LockfilePath, lf); err != nil {
						return nil, err
					}
				}
			}
			return &ConvertResult{
				Files:        files,
				TotalHoles:   cached.Stats.TotalHoles,
				FromLock:     cached.Stats.FromLock,
				FromAI:       cached.Stats.FromAI,
				FromStub:     cached.Stats.FromStub,
				Attempts:     attempts,
				Verified:     true,
				VerifyOutput: verifyOut,
			}, nil
		}

		lastErr = strings.TrimSpace(vOut)
		if lastErr == "" {
			lastErr = vErr.Error()
		}

		// Solo reintentar si hubo huecos resueltos por IA y quedan reintentos
		if !aiActive || len(cached.Stats.PendingLock) == 0 || attempt == maxRetries {
			break
		}

		bypass := make(map[string]bool, len(cached.Stats.PendingLock))
		for id := range cached.Stats.PendingLock {
			bypass[id] = true
		}
		cached.ResetAttempt(lastErr, bypass)
	}

	// Si falló tras todos los reintentos de IA, revertir a stubs seguros TODO(gnofy)
	// para nunca dejar código IA roto ni contaminar gnofy.lock.
	if aiActive && len(cached.Stats.PendingLock) > 0 {
		fallbackOpts := genOpts
		fallbackOpts.Resolver = ai.NoopResolver{}
		fbFiles, fbErr := gnogen.NewGenerator(fallbackOpts).GenerateContext(ctx, mod)
		if fbErr == nil {
			files = fbFiles
			if opts.OutDir != "" {
				_ = gnogen.WriteFiles(opts.OutDir, files)
			}
		}
		return &ConvertResult{
			Files:           files,
			TotalHoles:      cached.Stats.TotalHoles,
			FromLock:        0,
			FromAI:          0,
			FromStub:        cached.Stats.TotalHoles,
			Attempts:        attempts,
			Verified:        false,
			FellBackToStubs: true,
			VerifyOutput:    verifyOut,
			LastError:       lastErr,
		}, fmt.Errorf("verificación falló tras %d intento(s) IA; se revirtió a stubs TODO seguros:\n%s", attempts, lastErr)
	}

	return &ConvertResult{
		Files:        files,
		TotalHoles:   cached.Stats.TotalHoles,
		FromLock:     cached.Stats.FromLock,
		FromAI:       cached.Stats.FromAI,
		FromStub:     cached.Stats.FromStub,
		Attempts:     attempts,
		Verified:     false,
		VerifyOutput: verifyOut,
		LastError:    lastErr,
	}, fmt.Errorf("verificación falló:\n%s", lastErr)
}

func uniquePackageDirs(outDir string, files []gnogen.OutputFile) []string {
	seen := make(map[string]bool)
	var dirs []string
	for _, f := range files {
		if filepath.Base(f.RelPath) == "gnomod.toml" {
			d := filepath.Dir(filepath.Join(outDir, filepath.FromSlash(f.RelPath)))
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	if len(dirs) == 0 {
		return []string{outDir}
	}
	return dirs
}

func verifyAll(ctx context.Context, v Verifier, dirs []string) (string, error) {
	var out strings.Builder
	for _, d := range dirs {
		res, err := v.VerifyDir(ctx, d)
		if res != nil {
			if res.Stdout != "" {
				out.WriteString(res.Stdout)
			}
			if res.Stderr != "" {
				out.WriteString(res.Stderr)
			}
		}
		if err != nil {
			return out.String(), err
		}
	}
	return out.String(), nil
}
