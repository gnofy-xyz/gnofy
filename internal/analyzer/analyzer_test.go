package analyzer_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gnofy-xyz/gnofy/internal/analyzer"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
)

func compileAndAnalyze(t *testing.T, relPaths ...string) *analyzer.Report {
	t.Helper()
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}
	full := make([]string, len(relPaths))
	for i, p := range relPaths {
		full[i] = filepath.Join("..", "..", p)
	}
	runner := solc.NewRunner(solc.DefaultOptions())
	res, err := runner.Compile(context.Background(), full)
	if err != nil {
		t.Fatalf("Compile(%v) error: %v", relPaths, err)
	}
	return analyzer.Analyze(res)
}

func hasCode(findings []analyzer.Finding, code string) bool {
	for _, f := range findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestAnalyzeCounter(t *testing.T) {
	rep := compileAndAnalyze(t, "testdata/contracts/Counter.sol")
	if len(rep.Contracts) != 1 {
		t.Fatalf("esperaba 1 contrato, obtuve %d", len(rep.Contracts))
	}
	c := rep.Contracts[0]
	if c.Name != "Counter" || c.DeterministicPct != 100.0 || c.HoleCount != 0 {
		t.Fatalf("Counter debería ser 100%% determinista sin holes: %+v", c)
	}
}

func TestAnalyzeSimpleERC20(t *testing.T) {
	rep := compileAndAnalyze(t, "testdata/contracts/SimpleERC20.sol")
	if len(rep.Contracts) != 1 {
		t.Fatalf("esperaba 1 contrato, obtuve %d", len(rep.Contracts))
	}
	c := rep.Contracts[0]
	if c.Name != "SimpleERC20" || c.DeterministicPct != 100.0 || c.HoleCount != 0 {
		t.Fatalf("SimpleERC20 debería ser 100%% determinista: %+v", c)
	}
	if !hasCode(rep.Findings, "CONSTRUCTOR_WITH_ARGS") {
		t.Fatalf("esperaba warning CONSTRUCTOR_WITH_ARGS, findings=%+v", rep.Findings)
	}
	foundERC20 := false
	for _, s := range rep.DetectedStandards {
		if s == "ERC-20 (candidato a GRC-20)" {
			foundERC20 = true
		}
	}
	if !foundERC20 {
		t.Fatalf("esperaba detección de ERC-20 en SimpleERC20, got=%v", rep.DetectedStandards)
	}
}

func TestAnalyzeUnsupportedYul(t *testing.T) {
	rep := compileAndAnalyze(t, "testdata/contracts/UnsupportedYul.sol")
	if len(rep.Contracts) != 1 {
		t.Fatalf("esperaba 1 contrato, obtuve %d", len(rep.Contracts))
	}
	c := rep.Contracts[0]
	if c.DeterministicPct != 0.0 || c.HoleCount != 2 {
		t.Fatalf("UnsupportedYul debería tener 0%% determinista y 2 holes: %+v", c)
	}
	if !hasCode(rep.Findings, "UNSUPPORTED_YUL") || !hasCode(rep.Findings, "UNSUPPORTED_DELEGATECALL") {
		t.Fatalf("esperaba UNSUPPORTED_YUL y UNSUPPORTED_DELEGATECALL, findings=%+v", rep.Findings)
	}
}

func TestAnalyzeWideSignedIntIsHole(t *testing.T) {
	rep := compileAndAnalyze(t, "testdata/contracts/WideSignedInt.sol")
	if len(rep.Contracts) != 1 {
		t.Fatalf("esperaba 1 contrato, obtuve %d", len(rep.Contracts))
	}
	c := rep.Contracts[0]
	if c.HoleCount == 0 || c.DeterministicPct == 100.0 {
		t.Fatalf("WideSignedInt (int256) NO debe reportar 100%% determinista ni 0 holes: %+v", c)
	}
	if !hasCode(rep.Findings, "UNSUPPORTED_WIDE_SIGNED_INT") {
		t.Fatalf("esperaba finding UNSUPPORTED_WIDE_SIGNED_INT, findings=%+v", rep.Findings)
	}
}

func TestAnalyzeTokenVault(t *testing.T) {
	rep := compileAndAnalyze(t, "testdata/contracts/TokenVault.sol")
	if len(rep.Contracts) != 1 || rep.Contracts[0].Name != "TokenVault" {
		t.Fatalf("esperaba TokenVault, obtuve %+v", rep.Contracts)
	}
	if rep.Contracts[0].HoleCount != 0 || rep.Contracts[0].DeterministicPct != 100.0 {
		t.Fatalf("TokenVault debería ser 100%% determinista (sin holes), obtuve %+v", rep.Contracts[0])
	}
	for _, want := range []string{"SHIM_IMPORT_USED", "DYNAMIC_ERC20_CAST", "CONSTRUCTOR_WITH_ARGS", "EXTERNAL_SELF_CALL", "NATIVE_BALANCE_QUERY"} {
		if !hasCode(rep.Findings, want) {
			t.Errorf("esperaba finding %s en TokenVault", want)
		}
	}
}
