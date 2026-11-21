package solc_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
)

func TestFoundryRemappingsAndCompile(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}

	root := t.TempDir()
	libDir := filepath.Join(root, "lib", "mymath", "src")
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}

	foundryToml := `[profile.default]
src = "src"
libs = ["lib"]
remappings = ["@mymath/=lib/mymath/src/"]
`
	if err := os.WriteFile(filepath.Join(root, "foundry.toml"), []byte(foundryToml), 0o644); err != nil {
		t.Fatal(err)
	}

	libSol := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
library MyMath {
    function double(uint64 x) internal pure returns (uint64) { return x * 2; }
}
`
	if err := os.WriteFile(filepath.Join(libDir, "MyMath.sol"), []byte(libSol), 0o644); err != nil {
		t.Fatal(err)
	}

	appSol := `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;
import "@mymath/MyMath.sol";
contract App {
    function f(uint64 x) external pure returns (uint64) { return MyMath.double(x); }
}
`
	appPath := filepath.Join(srcDir, "App.sol")
	if err := os.WriteFile(appPath, []byte(appSol), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := solc.DefaultOptions()
	opts.UseShims = false // probar que resuelve 100% por foundry.toml
	res, err := solc.NewRunner(opts).Compile(context.Background(), []string{appPath})
	if err != nil {
		t.Fatalf("Compile con foundry.toml falló: %v", err)
	}
	if len(res.ShimmedImports) != 0 {
		t.Fatalf("no debía usar shims, got %v", res.ShimmedImports)
	}
}
