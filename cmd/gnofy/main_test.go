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
