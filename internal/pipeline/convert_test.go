package pipeline_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnofy-xyz/gnofy/internal/ai"
	gnogen "github.com/gnofy-xyz/gnofy/internal/codegen/gno"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
	"github.com/gnofy-xyz/gnofy/internal/pipeline"
	"github.com/gnofy-xyz/gnofy/internal/verify"
)

type mockVerifier struct {
	fn func(dir string) (*verify.Result, error)
}

func (m mockVerifier) VerifyDir(_ context.Context, dir string) (*verify.Result, error) {
	return m.fn(dir)
}

func TestRepairLoopSucceedsOnSecondAttemptAndLocks(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}

	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	lockPath := filepath.Join(tmp, "gnofy.lock")

	aiCalls := 0
	resolver := ai.HoleResolverFunc(func(_ context.Context, req ai.HoleRequest) (ai.HoleResolution, error) {
		aiCalls++
		if req.Hole.Reason == "UNSUPPORTED_YUL" {
			if req.PreviousError == "" {
				// Primer intento: código roto a propósito
				return ai.HoleResolution{
					GnoCode: "id = BROKEN_SYMBOL()",
					Model:   "mock-coder",
					Source:  ai.SourceAIFresh,
				}, nil
			}
			// Segundo intento: recibe el error de verificación y lo corrige
			return ai.HoleResolution{
				GnoCode: "id = U256FromU64(1)",
				Model:   "mock-coder",
				Source:  ai.SourceAIFresh,
			}, nil
		}
		return ai.HoleResolution{
			GnoCode: `[]byte(nil)`,
			Model:   "mock-coder",
			Source:  ai.SourceAIFresh,
		}, nil
	})

	verifier := mockVerifier{fn: func(dir string) (*verify.Result, error) {
		raw, err := os.ReadFile(filepath.Join(dir, "unsupportedyul.gno"))
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(raw), "BROKEN_SYMBOL") {
			return &verify.Result{Passed: false, Stdout: "undefined: BROKEN_SYMBOL"}, errors.New("typecheck failed")
		}
		return &verify.Result{Passed: true, Stdout: "PASS"}, nil
	}}

	res, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
		Inputs:       []string{filepath.Join("..", "..", "testdata/contracts/UnsupportedYul.sol")},
		OutDir:       outDir,
		Solc:         solc.DefaultOptions(),
		Codegen:      gnogen.DefaultOptions(),
		AIMode:       "assist",
		AIResolver:   resolver,
		LockfilePath: lockPath,
		Verify:       true,
		Verifier:     verifier,
		MaxRetries:   2,
	})
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}
	if !res.Verified || res.Attempts != 2 {
		t.Fatalf("esperaba Verified=true en 2 intentos, got verified=%v attempts=%d", res.Verified, res.Attempts)
	}

	// Verificar que gnofy.lock se guardó con entradas verificadas
	lf, err := ai.LoadLockfile(lockPath)
	if err != nil {
		t.Fatalf("LoadLockfile: %v", err)
	}
	if len(lf.Entries) == 0 {
		t.Fatalf("esperaba entradas en gnofy.lock")
	}
	for _, e := range lf.Entries {
		if !e.Verified {
			t.Errorf("entrada %s debería estar Verified=true", e.HoleID)
		}
	}

	// Segunda ejecución con el mismo lockfile: 0 llamadas nuevas a IA
	callsBefore := aiCalls
	res2, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
		Inputs:       []string{filepath.Join("..", "..", "testdata/contracts/UnsupportedYul.sol")},
		OutDir:       outDir,
		Solc:         solc.DefaultOptions(),
		Codegen:      gnogen.DefaultOptions(),
		AIMode:       "assist",
		AIResolver:   resolver,
		LockfilePath: lockPath,
		Verify:       true,
		Verifier:     verifier,
		MaxRetries:   2,
	})
	if err != nil {
		t.Fatalf("Convert #2 failed: %v", err)
	}
	if aiCalls != callsBefore || res2.FromLock == 0 || res2.FromAI != 0 {
		t.Fatalf("esperaba cache hit total desde gnofy.lock, callsBefore=%d callsAfter=%d fromLock=%d fromAI=%d",
			callsBefore, aiCalls, res2.FromLock, res2.FromAI)
	}
}

func TestRepairLoopFallsBackToStubsWhenAIFails(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}

	tmp := t.TempDir()
	outDir := filepath.Join(tmp, "out")
	lockPath := filepath.Join(tmp, "gnofy.lock")

	alwaysBroken := ai.HoleResolverFunc(func(_ context.Context, _ ai.HoleRequest) (ai.HoleResolution, error) {
		return ai.HoleResolution{
			GnoCode: "ALWAYS_BROKEN()",
			Model:   "mock-broken",
			Source:  ai.SourceAIFresh,
		}, nil
	})
	alwaysFailVerify := mockVerifier{fn: func(_ string) (*verify.Result, error) {
		return &verify.Result{Passed: false, Stdout: "undefined: ALWAYS_BROKEN"}, errors.New("fail")
	}}

	res, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
		Inputs:       []string{filepath.Join("..", "..", "testdata/contracts/UnsupportedYul.sol")},
		OutDir:       outDir,
		Solc:         solc.DefaultOptions(),
		Codegen:      gnogen.DefaultOptions(),
		AIMode:       "assist",
		AIResolver:   alwaysBroken,
		LockfilePath: lockPath,
		Verify:       true,
		Verifier:     alwaysFailVerify,
		MaxRetries:   1,
	})
	if err == nil {
		t.Fatalf("esperaba error tras agotar reintentos")
	}
	if !res.FellBackToStubs {
		t.Fatalf("esperaba FellBackToStubs=true")
	}
	// Confirmar que el archivo en disco quedó revertido a TODO(gnofy) seguro y gnofy.lock NO se contaminó
	raw, err := os.ReadFile(filepath.Join(outDir, "unsupportedyul.gno"))
	if err != nil {
		t.Fatalf("read fallback file: %v", err)
	}
	if !strings.Contains(string(raw), "TODO(gnofy)") {
		t.Fatalf("esperaba revert a stub TODO(gnofy) en disco:\n%s", string(raw))
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("gnofy.lock no debería crearse con resoluciones no verificadas")
	}
}

func TestConvertAIAssistAlwaysVerifiesAndAnnotates(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}

	outDir := t.TempDir()
	lockPath := filepath.Join(t.TempDir(), "gnofy.lock")

	resolver := ai.HoleResolverFunc(func(_ context.Context, req ai.HoleRequest) (ai.HoleResolution, error) {
		if req.Hole.Reason == "UNSUPPORTED_YUL" {
			return ai.HoleResolution{
				GnoCode: `id = U256FromU64(uint64(runtime.ChainHeight()))`,
				Model:   "mock-coder",
				Source:  ai.SourceAIFresh,
			}, nil
		}
		return ai.HoleResolution{
			GnoCode: `[]byte(nil)`,
			Model:   "mock-coder",
			Source:  ai.SourceAIFresh,
		}, nil
	})

	verifierCalled := false
	res, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
		Inputs:       []string{filepath.Join("..", "..", "testdata/contracts/UnsupportedYul.sol")},
		OutDir:       outDir,
		Solc:         solc.DefaultOptions(),
		Codegen:      gnogen.DefaultOptions(),
		AIMode:       "assist",
		AIResolver:   resolver,
		LockfilePath: lockPath,
		Verify:       false, // --ai=assist debe verificar SIEMPRE aunque Verify sea false
		Verifier: mockVerifier{fn: func(_ string) (*verify.Result, error) {
			verifierCalled = true
			return &verify.Result{Passed: true, Stdout: "PASS"}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Convert UnsupportedYul error: %v", err)
	}
	if !verifierCalled || !res.Verified {
		t.Fatalf("esperaba verificación obligatoria en --ai=assist incluso con Verify=false")
	}
	if res.TotalHoles != 2 || res.FromAI != 2 {
		t.Fatalf("esperaba 2 huecos resueltos por IA, got total=%d fromAI=%d", res.TotalHoles, res.FromAI)
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "unsupportedyul.gno"))
	if err != nil {
		t.Fatalf("read unsupportedyul.gno: %v", err)
	}
	src := string(raw)
	for _, want := range []string{
		`"chain/runtime"`,
		"BEGIN gnofy-ai",
		"END gnofy-ai",
		"runtime.ChainHeight()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("unsupportedyul.gno no contiene %q\n--- SALIDA ---\n%s", want, src)
		}
	}
}
