package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gnofy-xyz/gnofy/internal/ai"
	"github.com/gnofy-xyz/gnofy/internal/analyzer"
	gnogen "github.com/gnofy-xyz/gnofy/internal/codegen/gno"
	"github.com/gnofy-xyz/gnofy/internal/frontend/solc"
	"github.com/gnofy-xyz/gnofy/internal/pipeline"
	"github.com/gnofy-xyz/gnofy/internal/verify"
)

var version = "0.4.0-dev"

type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }
func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "analyze":
		if err := runAnalyze(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "convert":
		if err := runConvert(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "verify":
		if err := runVerify(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	case "version", "-v", "--version":
		fmt.Printf("gnofy %s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "comando desconocido: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "Emitir reporte en formato JSON")
	solcBin := fs.String("solc", "", "Ruta explícita al binario solc o solcjs")
	noShims := fs.Bool("no-shims", false, "Desactivar shims embebidos para @openzeppelin/@prb/math")
	strictPragma := fs.Bool("strict-pragma", false, "No relajar pragmas fijados 0.8.x si difieren de la versión de solc instalada")
	var includePaths stringSliceFlag
	fs.Var(&includePaths, "include-path", "Directorio adicional para resolver imports (repetible)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("uso: gnofy analyze [opciones] <archivo.sol | directorio> [...]")
	}

	runner := solc.NewRunner(solc.Options{
		BinaryPath:   *solcBin,
		IncludePaths: includePaths,
		UseShims:     !*noShims,
		RelaxPragma:  !*strictPragma,
	})
	compiled, err := runner.Compile(context.Background(), fs.Args())
	if err != nil {
		return err
	}

	rep := analyzer.Analyze(compiled)
	if *jsonOut {
		return analyzer.FormatJSON(os.Stdout, rep)
	}
	analyzer.FormatText(os.Stdout, rep)
	return nil
}

func runConvert(args []string) error {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	outDir := fs.String("o", "./out", "Directorio de salida para los archivos Gno generados")
	modPrefix := fs.String("module-prefix", "gno.land/r/gnofy", "Prefijo de ruta de módulo para gnomod.toml")
	idiomatic := fs.Bool("idiomatic", false, "Emitir Gno idiomático (ERC-20 -> GRC-20 nativo, ownable, Render Markdown vivo)")
	runtimeImport := fs.String("runtime-import", "", "Importar solrt externo (desactiva solrt_gen.gno embebido)")
	solcBin := fs.String("solc", "", "Ruta explícita al binario solc o solcjs")
	noShims := fs.Bool("no-shims", false, "Desactivar shims embebidos")
	strictPragma := fs.Bool("strict-pragma", false, "No relajar pragmas fijados 0.8.x")
	noRender := fs.Bool("no-render", false, "No generar stub Render(path string) string")
	noTests := fs.Bool("no-tests", false, "No generar *_test.gno")
	doVerify := fs.Bool("verify", false, "Ejecutar gno test automáticamente tras generar")
	gnoBin := fs.String("gno", "", "Ruta explícita al binario gno (para --verify o --ai=assist)")

	// Flags del motor leve de IA (Hito v0.4)
	aiMode := fs.String("ai", "off", "Modo de resolución de huecos: 'off' (100% determinista) o 'assist' (IA quirúrgica)")
	aiEndpoint := fs.String("ai-endpoint", envOr("GNOFY_AI_ENDPOINT", "http://localhost:11434/v1"), "Endpoint compatible OpenAI (/v1/chat/completions)")
	aiModel := fs.String("ai-model", envOr("GNOFY_AI_MODEL", "qwen2.5-coder:7b"), "Modelo IA para resolver huecos aislados")
	aiAPIKey := fs.String("ai-api-key", envOr("GNOFY_AI_API_KEY", os.Getenv("OPENAI_API_KEY")), "API key opcional para endpoint IA")
	lockfilePath := fs.String("lockfile", "gnofy.lock", "Ruta del lockfile para cachear resoluciones IA verificadas")
	maxRetries := fs.Int("max-retries", 2, "Reintentos máximos de reparación IA ante errores de gno test")

	var includePaths stringSliceFlag
	fs.Var(&includePaths, "include-path", "Directorio adicional para resolver imports (repetible)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("uso: gnofy convert [opciones] <archivo.sol | directorio> [...]")
	}
	if *aiMode != "off" && *aiMode != "assist" {
		return fmt.Errorf("--ai debe ser 'off' o 'assist' (recibido %q)", *aiMode)
	}

	genOpts := gnogen.DefaultOptions()
	genOpts.ModulePathPrefix = *modPrefix
	genOpts.Idiomatic = *idiomatic
	genOpts.GenerateRender = !*noRender
	genOpts.GenerateTests = !*noTests
	if *runtimeImport != "" {
		genOpts.EmbedRuntime = false
		genOpts.RuntimeImport = *runtimeImport
	}

	var resolver ai.HoleResolver = ai.NoopResolver{}
	if *aiMode == "assist" {
		resolver = ai.NewOpenAICompatResolver(ai.OpenAICompatConfig{
			Endpoint: *aiEndpoint,
			Model:    *aiModel,
			APIKey:   *aiAPIKey,
		})
	}

	var verifier pipeline.Verifier
	if *doVerify || *gnoBin != "" {
		verifier = &verify.Verifier{BinaryPath: *gnoBin}
	}

	res, err := pipeline.Convert(context.Background(), pipeline.ConvertOptions{
		Inputs: fs.Args(),
		OutDir: *outDir,
		Solc: solc.Options{
			BinaryPath:   *solcBin,
			IncludePaths: includePaths,
			UseShims:     !*noShims,
			RelaxPragma:  !*strictPragma,
		},
		Codegen:      genOpts,
		AIMode:       *aiMode,
		AIResolver:   resolver,
		LockfilePath: *lockfilePath,
		Verify:       *doVerify,
		Verifier:     verifier,
		MaxRetries:   *maxRetries,
	})

	if res != nil {
		fmt.Printf("gnofy convert: generados %d archivos en %s (intentos: %d)\n", len(res.Files), *outDir, res.Attempts)
		for _, f := range res.Files {
			fmt.Printf("  - %s\n", f.RelPath)
		}
		if res.TotalHoles > 0 {
			fmt.Printf("Huecos: %d total (lock: %d, ai: %d, stub TODO: %d)\n",
				res.TotalHoles, res.FromLock, res.FromAI, res.FromStub)
		}
		if res.VerifyOutput != "" {
			fmt.Print(res.VerifyOutput)
		}
	}
	return err
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	gnoBin := fs.String("gno", "", "Ruta explícita al binario gno")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}

	v := &verify.Verifier{BinaryPath: *gnoBin}
	res, err := v.VerifyDir(context.Background(), dir)
	if res != nil && res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res != nil && res.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Stderr)
	}
	return err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func printUsage() {
	fmt.Print(`gnofy — Transpilador determinista y suite de migración Solidity → Gno (gnofy.xyz)

Uso:
  gnofy <comando> [opciones]

Comandos:
  analyze <ruta...>  Audita contratos Solidity y reporta compatibilidad de migración
  convert <ruta...>  Transpila Solidity a Gno (--ai=off por defecto; --ai=assist opcional)
  verify  [dir]      Ejecuta 'gno test' sobre el paquete/realm generado
  version            Muestra la versión de gnofy
`)
}
