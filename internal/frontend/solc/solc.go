// Package solc encapsula la invocación a solc / solcjs (--standard-json) y la
// deserialización tipada de su AST oficial.
package solc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Options configura el comportamiento del frontend solc/solcjs.
type Options struct {
	BinaryPath   string      // opcional: fuerza un binario concreto (solc o solcjs)
	IncludePaths []string    // directorios adicionales para resolver imports (p.ej. node_modules, lib/)
	Remappings   []Remapping // remappings explícitos estilo Foundry (prefix=target)
	UseShims     bool        // resuelve imports conocidos (@openzeppelin, @prb/math) mediante shims embebidos
	RelaxPragma  bool        // si falla solo por pragma exacto 0.8.x, reintenta relajando a >=0.8.0
}

// DefaultOptions devuelve las opciones por defecto para análisis y transpilación.
func DefaultOptions() Options {
	return Options{
		UseShims:    true,
		RelaxPragma: true,
	}
}

// SourceOutput almacena el AST y metadatos de un archivo fuente compilado.
type SourceOutput struct {
	ID      int
	Path    string
	Content string
	AST     *Node
	IsShim  bool
}

// CompileResult contiene los ASTs resueltos, archivos objetivo y metadatos de compilación.
type CompileResult struct {
	CompilerBinary string
	TargetFiles    []string
	Sources        map[string]*SourceOutput
	Errors         []Diagnostic
	ShimmedImports []string
	RelaxedPragmas map[string]string // archivo -> pragma original relajado
}

// Diagnostic representa un error o advertencia emitido por solc.
type Diagnostic struct {
	Severity         string `json:"severity"`
	Type             string `json:"type"`
	ErrorCode        string `json:"errorCode"`
	Message          string `json:"message"`
	FormattedMessage string `json:"formattedMessage"`
}

// Compiler abstrae la ejecución de solc para facilitar tests offline.
type Compiler interface {
	Compile(ctx context.Context, paths []string) (*CompileResult, error)
}

// Runner ejecuta solc o solcjs en modo --standard-json.
type Runner struct {
	opts Options
}

// NewRunner crea un nuevo Runner con las opciones indicadas.
func NewRunner(opts Options) *Runner {
	return &Runner{opts: opts}
}

type standardJSONInput struct {
	Language string                       `json:"language"`
	Sources  map[string]standardJSONSrc   `json:"sources"`
	Settings standardJSONSettings         `json:"settings"`
}

type standardJSONSrc struct {
	Content string `json:"content"`
}

type standardJSONSettings struct {
	OutputSelection map[string]map[string][]string `json:"outputSelection"`
}

type standardJSONOutput struct {
	Errors  []Diagnostic `json:"errors"`
	Sources map[string]struct {
		ID  int   `json:"id"`
		AST *Node `json:"ast"`
	} `json:"sources"`
}

var (
	importPattern = regexp.MustCompile(`(?m)^\s*import\s+(?:[^"';]+\s+from\s+)?["']([^"']+)["']\s*;`)
	pragmaPattern = regexp.MustCompile(`(?m)^\s*pragma\s+solidity\s+(=?\s*0\.8\.\d+)\s*;`)
)

// ResolveBinary busca solc o solcjs en el PATH si BinaryPath no fue especificado.
func ResolveBinary(preferred string) (string, error) {
	if preferred != "" {
		return preferred, nil
	}
	for _, candidate := range []string{"solc", "solcjs"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("no se encontró 'solc' ni 'solcjs' en el PATH")
}

// CollectSolidityFiles expande una lista de archivos y/o directorios a archivos .sol ordenados.
func CollectSolidityFiles(inputs []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string

	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			return nil, fmt.Errorf("no se pudo leer %q: %w", in, err)
		}
		if !info.IsDir() {
			clean := filepath.ToSlash(filepath.Clean(in))
			if _, ok := seen[clean]; !ok {
				seen[clean] = struct{}{}
				files = append(files, clean)
			}
			continue
		}

		err = filepath.WalkDir(in, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "out" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) == ".sol" {
				clean := filepath.ToSlash(filepath.Clean(path))
				if _, ok := seen[clean]; !ok {
					seen[clean] = struct{}{}
					files = append(files, clean)
				}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("error recorriendo %q: %w", in, err)
		}
	}

	slices.Sort(files)
	if len(files) == 0 {
		return nil, errors.New("no se encontraron archivos .sol en las rutas indicadas")
	}
	return files, nil
}

// Compile compila los archivos o directorios indicados con solc/solcjs --standard-json
// y devuelve los ASTs completos.
func (r *Runner) Compile(ctx context.Context, paths []string) (*CompileResult, error) {
	bin, err := ResolveBinary(r.opts.BinaryPath)
	if err != nil {
		return nil, err
	}

	targetFiles, err := CollectSolidityFiles(paths)
	if err != nil {
		return nil, err
	}

	sources := make(map[string]string)
	isShim := make(map[string]bool)
	var shimmed []string

	for _, f := range targetFiles {
		raw, err := os.ReadFile(filepath.FromSlash(f))
		if err != nil {
			return nil, fmt.Errorf("error leyendo %q: %w", f, err)
		}
		sources[f] = string(raw)
	}

	// Descubrir automáticamente foundry.toml / remappings.txt desde el primer archivo
	remappings := slices.Clone(r.opts.Remappings)
	includePaths := slices.Clone(r.opts.IncludePaths)
	if len(targetFiles) > 0 {
		fRemaps, fIncludes := DiscoverFoundryConfig(filepath.Dir(filepath.FromSlash(targetFiles[0])))
		remappings = append(remappings, fRemaps...)
		includePaths = append(includePaths, fIncludes...)
	}

	if err := r.resolveImports(sources, isShim, &shimmed, remappings, includePaths); err != nil {
		return nil, err
	}

	out, err := r.runStandardJSON(ctx, bin, sources)
	if err != nil {
		return nil, err
	}

	relaxed := make(map[string]string)
	if r.opts.RelaxPragma && hasOnlyPragmaMismatchErrors(out.Errors) {
		changed := false
		for name, content := range sources {
			if isShim[name] {
				continue
			}
			if newContent, orig, ok := relaxPinnedPragma(content); ok {
				sources[name] = newContent
				relaxed[name] = orig
				changed = true
			}
		}
		if changed {
			out, err = r.runStandardJSON(ctx, bin, sources)
			if err != nil {
				return nil, err
			}
		}
	}

	var fatal []string
	for _, d := range out.Errors {
		if d.Severity == "error" {
			msg := strings.TrimSpace(d.FormattedMessage)
			if msg == "" {
				msg = d.Message
			}
			fatal = append(fatal, msg)
		}
	}
	if len(fatal) > 0 {
		return nil, fmt.Errorf("errores de compilación solc:\n%s", strings.Join(fatal, "\n"))
	}

	slices.Sort(shimmed)
	res := &CompileResult{
		CompilerBinary: bin,
		TargetFiles:    targetFiles,
		Sources:        make(map[string]*SourceOutput, len(out.Sources)),
		Errors:         out.Errors,
		ShimmedImports: shimmed,
		RelaxedPragmas: relaxed,
	}

	for name, srcOut := range out.Sources {
		res.Sources[name] = &SourceOutput{
			ID:      srcOut.ID,
			Path:    name,
			Content: sources[name],
			AST:     srcOut.AST,
			IsShim:  isShim[name],
		}
	}

	return res, nil
}

func (r *Runner) resolveImports(sources map[string]string, isShim map[string]bool, shimmed *[]string, remappings []Remapping, includePaths []string) error {
	queue := make([]string, 0, len(sources))
	for k := range sources {
		queue = append(queue, k)
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		matches := importPattern.FindAllStringSubmatch(sources[curr], -1)
		for _, m := range matches {
			imp := m[1]
			key, content, usedShim, found := r.loadImport(curr, imp, remappings, includePaths)
			if !found {
				continue // dejar que solc reporte el error exacto si falta
			}
			if _, exists := sources[key]; !exists {
				sources[key] = content
				if usedShim {
					isShim[key] = true
					*shimmed = append(*shimmed, key)
				}
				queue = append(queue, key)
			}
		}
	}
	return nil
}

func (r *Runner) loadImport(fromFile, imp string, remappings []Remapping, includePaths []string) (key, content string, usedShim, found bool) {
	// 1. Ruta relativa al archivo importador
	if strings.HasPrefix(imp, "./") || strings.HasPrefix(imp, "../") {
		cand := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(fromFile), imp)))
		if raw, err := os.ReadFile(filepath.FromSlash(cand)); err == nil {
			return cand, string(raw), false, true
		}
	}

	// 2. Remappings estilo Foundry (remappings.txt / foundry.toml / explícitos)
	if mapped, ok := applyRemappings(imp, remappings); ok {
		if raw, err := os.ReadFile(mapped); err == nil {
			return imp, string(raw), false, true
		}
	}

	// 3. IncludePaths configurados (p.ej. node_modules, lib/)
	for _, inc := range includePaths {
		cand := filepath.Join(inc, filepath.FromSlash(imp))
		if raw, err := os.ReadFile(cand); err == nil {
			return imp, string(raw), false, true
		}
	}

	// 4. Archivo directo relativo al workspace
	if raw, err := os.ReadFile(filepath.FromSlash(imp)); err == nil {
		return imp, string(raw), false, true
	}

	// 5. Shims embebidos de librerías estándar comunes
	if r.opts.UseShims {
		if shim, ok := BuiltinShims[imp]; ok {
			return imp, shim, true, true
		}
	}

	return "", "", false, false
}

func (r *Runner) runStandardJSON(ctx context.Context, bin string, sources map[string]string) (*standardJSONOutput, error) {
	in := standardJSONInput{
		Language: "Solidity",
		Sources:  make(map[string]standardJSONSrc, len(sources)),
		Settings: standardJSONSettings{
			OutputSelection: map[string]map[string][]string{
				"*": {
					"": {"ast"},
				},
			},
		},
	}
	for k, v := range sources {
		in.Sources[k] = standardJSONSrc{Content: v}
	}

	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("error serializando Standard JSON: %w", err)
	}

	// Redirigir stdout a archivo temporal evita truncamiento a 64 KiB del pipe
	// cuando solcjs (Bun/Node) hace process.exit() antes de vaciar stdout.
	tmpOut, err := os.CreateTemp("", "gnofy-solc-*.json")
	if err != nil {
		return nil, fmt.Errorf("error creando archivo temporal para salida de %s: %w", bin, err)
	}
	tmpPath := tmpOut.Name()
	defer os.Remove(tmpPath)

	cmd := exec.CommandContext(ctx, bin, "--standard-json")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Stdout = tmpOut

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	_ = tmpOut.Close()
	if runErr != nil {
		return nil, fmt.Errorf("error ejecutando %s --standard-json: %w\n%s", bin, runErr, strings.TrimSpace(stderr.String()))
	}

	rawOut, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("error leyendo salida de %s: %w", bin, err)
	}
	if idx := bytes.IndexByte(rawOut, '{'); idx >= 0 {
		rawOut = rawOut[idx:]
	}

	var out standardJSONOutput
	if err := json.Unmarshal(rawOut, &out); err != nil {
		return nil, fmt.Errorf("error parseando salida JSON de %s: %w", bin, err)
	}
	return &out, nil
}

func hasOnlyPragmaMismatchErrors(diags []Diagnostic) bool {
	hasPragmaErr := false
	for _, d := range diags {
		if d.Severity != "error" {
			continue
		}
		if d.ErrorCode == "5333" || strings.Contains(d.Message, "requires different compiler version") {
			hasPragmaErr = true
			continue
		}
		return false
	}
	return hasPragmaErr
}

func relaxPinnedPragma(content string) (updated, original string, ok bool) {
	loc := pragmaPattern.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, "", false
	}
	original = strings.TrimSpace(content[loc[2]:loc[3]])
	updated = content[:loc[2]] + ">=0.8.0" + content[loc[3]:]
	return updated, original, true
}
