package gno_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	gnogen "github.com/gnofy-xyz/gnofy/internal/codegen/gno"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
	"github.com/gnofy-xyz/gnofy/internal/ir"
	"github.com/gnofy-xyz/gnofy/internal/passes"
)

func assertValidGno117Syntax(t *testing.T, files []gnogen.OutputFile) {
	t.Helper()
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.RelPath, ".gno") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.RelPath, f.Content, parser.AllErrors)
		if err != nil {
			t.Fatalf("%s tiene errores de sintaxis Go/Gno: %v\n%s", f.RelPath, err, string(f.Content))
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncType:
				if x.TypeParams != nil && len(x.TypeParams.List) > 0 {
					t.Errorf("%s usa generics (Go 1.18+) no soportados por Gno", f.RelPath)
				}
			case *ast.TypeSpec:
				if x.TypeParams != nil && len(x.TypeParams.List) > 0 {
					t.Errorf("%s usa type params (Go 1.18+) no soportados por Gno", f.RelPath)
				}
			case *ast.GoStmt:
				t.Errorf("%s usa goroutine 'go' no soportada por Gno", f.RelPath)
			case *ast.ChanType:
				t.Errorf("%s usa chan no soportado por Gno", f.RelPath)
			}
			return true
		})
	}
}

func convertFixture(t *testing.T, relPath string) []gnogen.OutputFile {
	t.Helper()
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}
	runner := solc.NewRunner(solc.DefaultOptions())
	compiled, err := runner.Compile(context.Background(), []string{filepath.Join("..", "..", "..", relPath)})
	if err != nil {
		t.Fatalf("Compile(%s) error: %v", relPath, err)
	}
	mod := ir.Lower(compiled)
	if err := passes.DefaultPipeline().Run(mod); err != nil {
		t.Fatalf("Pipeline(%s) error: %v", relPath, err)
	}
	files, err := gnogen.NewGenerator(gnogen.DefaultOptions()).Generate(mod)
	if err != nil {
		t.Fatalf("Generate(%s) error: %v", relPath, err)
	}
	assertValidGno117Syntax(t, files)
	return files
}

func findFile(t *testing.T, files []gnogen.OutputFile, name string) string {
	t.Helper()
	for _, f := range files {
		if f.RelPath == name {
			return string(f.Content)
		}
	}
	t.Fatalf("archivo esperado %q no encontrado en salida (%d archivos)", name, len(files))
	return ""
}

func TestConvertCounter(t *testing.T) {
	files := convertFixture(t, "testdata/contracts/Counter.sol")
	src := findFile(t, files, "counter.gno")

	for _, want := range []string{
		"package counter",
		`"chain"`,
		"var (",
		"count uint64",
		"func Increment(cur realm)",
		"SafeAddU64(count, 1)",
		"cur.Previous().Address()",
		`chain.Emit("Incremented"`,
		"func Decrement(cur realm)",
		`Require((count > 0), "Counter: underflow")`,
		"func Get() uint64",
		"func Count() uint64",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("counter.gno no contiene %q\n--- SALIDA ---\n%s", want, src)
		}
	}
}

func TestConvertSimpleERC20(t *testing.T) {
	files := convertFixture(t, "testdata/contracts/SimpleERC20.sol")
	src := findFile(t, files, "simpleerc20.gno")

	for _, want := range []string{
		"package simpleerc20",
		"balanceOf",
		"allowance",
		"NewOrderedMap()",
		"func Init(cur realm, nameParam string, symbolParam string, initialSupply U256)",
		"name = nameParam",
		"symbol = symbolParam",
		"Require(!initialized",
		"func Transfer(cur realm, to address, amount U256) bool",
		"func Approve(cur realm, spender address, amount U256) bool",
		"func TransferFrom(cur realm, from address, to address, amount U256) bool",
		"func Mint(cur realm, to address, amount U256)",
		"Require((cur.Previous().Address() == owner)",
		"KeyJoin(",
		"func BalanceOf(k0 address) U256",
		"func Allowance(k0 address, k1 address) U256",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("simpleerc20.gno no contiene %q\n--- SALIDA ---\n%s", want, src)
		}
	}
}

func TestConvertUnsupportedYulEmitsSafeStubs(t *testing.T) {
	files := convertFixture(t, "testdata/contracts/UnsupportedYul.sol")
	src := findFile(t, files, "unsupportedyul.gno")

	if !strings.Contains(src, `TODO(gnofy)`) || !strings.Contains(src, "UNSUPPORTED_YUL") {
		t.Fatalf("esperaba stub TODO(gnofy) para UNSUPPORTED_YUL:\n%s", src)
	}
}

func TestConvertWideSignedIntEmitsHoleInsteadOfTruncating(t *testing.T) {
	files := convertFixture(t, "testdata/contracts/WideSignedInt.sol")
	src := findFile(t, files, "widesignedint.gno")

	if !strings.Contains(src, "UNSUPPORTED_WIDE_SIGNED_INT") {
		t.Fatalf("esperaba marcador/stub UNSUPPORTED_WIDE_SIGNED_INT en widesignedint.gno:\n%s", src)
	}
	if strings.Contains(src, "delta int64") {
		t.Fatalf("widesignedint.gno recortó int256 a int64 silenciosamente:\n%s", src)
	}
}

func TestConvertMultiInherit(t *testing.T) {
	files := convertFixture(t, "testdata/contracts/MultiInherit.sol")
	src := findFile(t, files, "multiinherit.gno")

	for _, want := range []string{
		"package multiinherit",
		"baseVal uint64 = 10",
		"func Compute() uint64",
		"superBaseBCompute()",
		"superBaseACompute()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("multiinherit.gno no contiene %q\n--- SALIDA ---\n%s", want, src)
		}
	}
}

func TestConvertIdiomaticSimpleERC20GRC20(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}
	runner := solc.NewRunner(solc.DefaultOptions())
	compiled, err := runner.Compile(context.Background(), []string{filepath.Join("..", "..", "..", "testdata/contracts/SimpleERC20.sol")})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	mod := ir.Lower(compiled)
	if err := passes.IdiomaticPipeline().Run(mod); err != nil {
		t.Fatalf("IdiomaticPipeline: %v", err)
	}
	opts := gnogen.DefaultOptions()
	opts.Idiomatic = true
	files, err := gnogen.NewGenerator(opts).Generate(mod)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertValidGno117Syntax(t, files)

	// No debe emitir solrt_gen.gno para un GRC-20 nativo
	for _, f := range files {
		if f.RelPath == "solrt_gen.gno" {
			t.Fatalf("GRC-20 idiomático no debería necesitar solrt_gen.gno")
		}
	}

	src := findFile(t, files, "simpleerc20.gno")
	for _, want := range []string{
		`"gno.land/p/nt/grc20/v0"`,
		`"gno.land/p/nt/ownable/v0"`,
		"grc20.NewToken(nameParam, symbolParam, 18, 0, cur)",
		"Ownable = ownable.NewWithAddress(caller)",
		"Ownable.AssertOwnedBy(cur.Previous().Address())",
		"ledger.Transfer(",
		"ledger.Approve(",
		"ledger.TransferFrom(",
		"Token.RenderHome()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("simpleerc20.gno idiomático no contiene %q\n--- SALIDA ---\n%s", want, src)
		}
	}
}

func TestConvertIdiomaticRenderMarkdown(t *testing.T) {
	if _, err := solc.ResolveBinary(""); err != nil {
		t.Skipf("solc/solcjs no disponible: %v", err)
	}
	runner := solc.NewRunner(solc.DefaultOptions())
	compiled, err := runner.Compile(context.Background(), []string{filepath.Join("..", "..", "..", "testdata/contracts/Counter.sol")})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	mod := ir.Lower(compiled)
	if err := passes.IdiomaticPipeline().Run(mod); err != nil {
		t.Fatalf("IdiomaticPipeline: %v", err)
	}
	opts := gnogen.DefaultOptions()
	opts.Idiomatic = true
	files, err := gnogen.NewGenerator(opts).Generate(mod)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertValidGno117Syntax(t, files)
	src := findFile(t, files, "counter.gno")
	if !strings.Contains(src, "| Campo | Valor |") || !strings.Contains(src, "`count`") {
		t.Fatalf("esperaba tabla Markdown en Render() idiomático:\n%s", src)
	}
}

