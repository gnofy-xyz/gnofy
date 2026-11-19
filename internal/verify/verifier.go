// Package verify ejecuta el toolchain 'gno' (gno test / type-check) sobre los
// paquetes o realms generados para validar que compilan y pasan pruebas.
package verify

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Result recoge la salida de verificación de `gno test`.
type Result struct {
	Passed bool
	Stdout string
	Stderr string
}

// Verifier invoca el binario `gno` disponible en el PATH o en BinaryPath.
type Verifier struct {
	BinaryPath string
}

// VerifyDir ejecuta `gno test -v ./...` dentro del directorio objetivo.
func (v *Verifier) VerifyDir(ctx context.Context, dir string) (*Result, error) {
	bin := v.BinaryPath
	if bin == "" {
		bin = "gno"
	}

	cmd := exec.CommandContext(ctx, bin, "test", "-v", "./...")
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	res := &Result{
		Passed: err == nil,
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		combined := res.Stdout + "\n" + res.Stderr
		if bytes.Contains([]byte(combined), []byte("/__w/gno/gno/gnovm/stdlibs")) ||
			bytes.Contains([]byte(combined), []byte("is not in std")) {
			return res, fmt.Errorf("gno test falló porque GNOROOT no apunta a las stdlibs de gno (ejecuta 'git clone --depth=1 https://github.com/gnolang/gno ~/.gno/src && export GNOROOT=$HOME/.gno/src'): %w", err)
		}
		return res, fmt.Errorf("gno test falló: %w", err)
	}
	return res, nil
}
