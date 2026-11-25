package verify_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gnogen "github.com/gnofy-xyz/gnofy/internal/codegen/gno"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
	"github.com/gnofy-xyz/gnofy/internal/pipeline"
	"github.com/gnofy-xyz/gnofy/internal/verify"
)

func resolveGnoBinary(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("gno"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		cand := filepath.Join(home, ".gno", "bin", "gno")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	t.Skip("binario 'gno' no disponible en PATH ni en ~/.gno/bin; omitiendo tests funcionales GnoVM")
	return ""
}

func TestFunctionalGnoVMLifecycles(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}
	gnoBin := resolveGnoBinary(t)
	if os.Getenv("GNOROOT") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			src := filepath.Join(home, ".gno", "src")
			if _, err := os.Stat(src); err == nil {
				t.Setenv("GNOROOT", src)
			}
		}
	}

	cases := []struct {
		name      string
		solFile   string
		idiomatic bool
		flowTest  string
	}{
		{
			name:      "Counter",
			solFile:   "testdata/contracts/Counter.sol",
			idiomatic: true,
			flowTest:  "testdata/functional/counter_flow_test.gno",
		},
		{
			name:      "MultiInherit_C3",
			solFile:   "testdata/contracts/MultiInherit.sol",
			idiomatic: false,
			flowTest:  "testdata/functional/multiinherit_flow_test.gno",
		},
		{
			name:      "SimpleERC20_Strict",
			solFile:   "testdata/contracts/SimpleERC20.sol",
			idiomatic: false,
			flowTest:  "testdata/functional/simpleerc20_strict_flow_test.gno",
		},
		{
			name:      "SimpleERC20_Idiomatic_GRC20",
			solFile:   "testdata/contracts/SimpleERC20.sol",
			idiomatic: true,
			flowTest:  "testdata/functional/simpleerc20_idiomatic_flow_test.gno",
		},
	}

	v := &verify.Verifier{BinaryPath: gnoBin}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outDir := t.TempDir()
			codeOpts := gnogen.DefaultOptions()
			codeOpts.Idiomatic = tc.idiomatic

			_, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
				Inputs:  []string{filepath.Join("..", "..", tc.solFile)},
				OutDir:  outDir,
				Solc:    solc.DefaultOptions(),
				Codegen: codeOpts,
				AIMode:  "off",
			})
			if err != nil {
				t.Fatalf("Convert(%s) error: %v", tc.solFile, err)
			}

			// Copiar el test funcional GnoVM al paquete recién transpilado
			testSrc, err := os.ReadFile(filepath.Join("..", "..", tc.flowTest))
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", tc.flowTest, err)
			}
			dest := filepath.Join(outDir, filepath.Base(tc.flowTest))
			if err := os.WriteFile(dest, testSrc, 0o644); err != nil {
				t.Fatalf("WriteFile(%s): %v", dest, err)
			}

			res, err := v.VerifyDir(context.Background(), outDir)
			if err != nil {
				t.Fatalf("gno test falló para %s:\n%s\n%s\nerr=%v", tc.name, res.Stdout, res.Stderr, err)
			}
			t.Logf("GnoVM output (%s):\n%s", tc.name, res.Stdout)
		})
	}
}
