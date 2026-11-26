package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHelpFlagsExitZeroAndNeverLeakSecrets(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "gnofy-test-bin")
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	build := exec.Command(goBin, "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}

	for _, sub := range []string{"analyze", "convert", "verify"} {
		cmd := exec.Command(bin, sub, "-h")
		cmd.Env = append(os.Environ(),
			"OPENAI_API_KEY=sk-secret-openai-99999",
			"GNOFY_AI_API_KEY=sk-secret-gnofy-88888",
		)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out

		if err := cmd.Run(); err != nil {
			t.Errorf("gnofy %s -h debe salir con exit 0, error=%v\n%s", sub, err, out.String())
		}
		text := out.String()
		if strings.Contains(text, "sk-secret-openai") || strings.Contains(text, "sk-secret-gnofy") {
			t.Errorf("gnofy %s -h filtró una API key en la salida de ayuda:\n%s", sub, text)
		}
		if strings.Contains(text, "flag: help requested") {
			t.Errorf("gnofy %s -h no debe imprimir 'flag: help requested':\n%s", sub, text)
		}
	}
}

func TestConvertAcceptsFlagsAfterPositionalArgs(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "gnofy-test-bin")
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}

	outDir := filepath.Join(t.TempDir(), "counter")
	solPath := filepath.Join("..", "..", "testdata", "contracts", "Counter.sol")
	cmd := exec.Command(bin, "convert", solPath, "-o", outDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("gnofy convert <file> -o <dir> failed: %v\n%s", err, string(out))
	}
	if _, err := os.Stat(filepath.Join(outDir, "counter.gno")); err != nil {
		t.Fatalf("expected counter.gno in %s: %v", outDir, err)
	}
}
