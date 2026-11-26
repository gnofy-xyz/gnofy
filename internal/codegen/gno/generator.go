// Package gno emite código fuente estrictamente compatible con Gno (Go 1.17 + stdlib Gno),
// junto con gnomod.toml, solrt_gen.gno (si EmbedRuntime), stubs de Render() y *_test.gno.
package gno

import (
	"bytes"
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	gnoruntime "github.com/gnofy-xyz/gnofy/gno"
	"github.com/gnofy-xyz/gnofy/internal/ai"
	"github.com/gnofy-xyz/gnofy/internal/ir"
)

// Options configura el comportamiento del generador de código Gno.
type Options struct {
	ModulePathPrefix string // ej. "gno.land/r/gnofy" -> genera "gno.land/r/gnofy/<pkg>"
	Idiomatic        bool   // activa mapeos nativos de gno.land (GRC-20, ownable, Render Markdown vivo)
	EmbedRuntime     bool   // true: embebe solrt_gen.gno en el paquete para gno test offline
	RuntimeImport    string // usado solo si EmbedRuntime == false (ej. "gno.land/p/gnofy/solrt")
	GenerateRender   bool   // genera Render(path string) string
	GenerateTests    bool   // genera <pkg>_test.gno
	Resolver         ai.HoleResolver
}

// DefaultOptions devuelve opciones listas para desarrollo y verificación offline con gno test.
func DefaultOptions() Options {
	return Options{
		ModulePathPrefix: "gno.land/r/gnofy",
		EmbedRuntime:     true,
		RuntimeImport:    "gno.land/p/gnofy/solrt",
		GenerateRender:   true,
		GenerateTests:    true,
		Resolver:         ai.NoopResolver{},
	}
}

// OutputFile representa un archivo generado en memoria listo para escribirse a disco.
type OutputFile struct {
	RelPath string
	Content []byte
}

// Generator convierte una Module IR ya transformada en archivos fuente Gno.
type Generator struct {
	opts Options
}

// NewGenerator crea un generador con las opciones indicadas.
func NewGenerator(opts Options) *Generator {
	if opts.ModulePathPrefix == "" {
		opts.ModulePathPrefix = "gno.land/r/gnofy"
	}
	if opts.RuntimeImport == "" {
		opts.RuntimeImport = "gno.land/p/gnofy/solrt"
	}
	if opts.Resolver == nil {
		opts.Resolver = ai.NoopResolver{}
	}
	return &Generator{opts: opts}
}

// Generate produce todos los archivos Gno para los contratos concretos de la Module IR.
func (g *Generator) Generate(mod *ir.Module) ([]OutputFile, error) {
	return g.GenerateContext(context.Background(), mod)
}

// GenerateContext produce todos los archivos Gno respetando cancelación/timeout de ctx.
func (g *Generator) GenerateContext(ctx context.Context, mod *ir.Module) ([]OutputFile, error) {
	var targets []*ir.Contract
	for _, c := range mod.Contracts {
		if c.Kind == ir.KindInterface || c.Abstract {
			continue
		}
		targets = append(targets, c)
	}

	var files []OutputFile
	multi := len(targets) > 1

	for _, c := range targets {
		prefix := ""
		if multi {
			prefix = c.GoPkgName
		}

		cFiles, err := g.generateContract(ctx, c, prefix)
		if err != nil {
			return nil, fmt.Errorf("contrato %s: %w", c.Name, err)
		}
		files = append(files, cFiles...)
	}
	return files, nil
}

// WriteFiles escribe los OutputFile bajo el directorio raíz outDir, limpiando
// archivos .gno y gnomod.toml obsoletos de ejecuciones previas en esa carpeta.
func WriteFiles(outDir string, files []OutputFile) error {
	keep := make(map[string]bool, len(files))
	for _, f := range files {
		keep[filepath.ToSlash(filepath.Clean(f.RelPath))] = true
	}

	if err := cleanStaleGnoFiles(outDir, keep); err != nil {
		return err
	}

	for _, f := range files {
		full := filepath.Join(outDir, filepath.FromSlash(f.RelPath))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", full, err)
		}
	}
	return nil
}

func cleanStaleGnoFiles(outDir string, keep map[string]bool) error {
	info, err := os.Stat(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", outDir, err)
	}
	if !info.IsDir() {
		return nil
	}

	// Guarda de seguridad: nunca purgar archivos si outDir parece ser la raíz de un repo Go/Git
	if _, err := os.Stat(filepath.Join(outDir, ".git")); err == nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(outDir, "go.mod")); err == nil {
		return nil
	}

	var dirs []string
	err = filepath.WalkDir(outDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == outDir {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(filepath.Clean(rel))
		if keep[relSlash] {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".gno") || base == "gnomod.toml" {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("remove stale %s: %w", path, err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Eliminar subdirectorios que hayan quedado vacíos tras purgar contratos antiguos
	for i := len(dirs) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(dirs[i])
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dirs[i])
		}
	}
	return nil
}

func (g *Generator) generateContract(ctx context.Context, c *ir.Contract, relDir string) ([]OutputFile, error) {
	join := func(name string) string {
		if relDir == "" {
			return name
		}
		return relDir + "/" + name
	}

	var out []OutputFile

	// 1. gnomod.toml
	modPath := strings.TrimRight(g.opts.ModulePathPrefix, "/") + "/" + c.GoPkgName
	gnomod := fmt.Sprintf("module = %q\ngno = \"0.9\"\n", modPath)
	out = append(out, OutputFile{
		RelPath: join("gnomod.toml"),
		Content: []byte(gnomod),
	})

	// 2. Modo idiomático GRC-20 nativo (sin solrt_gen.gno!)
	if g.opts.Idiomatic && c.IdiomaticGRC20 {
		grcSrc, err := g.emitIdiomaticGRC20(c)
		if err != nil {
			return nil, err
		}
		out = append(out, OutputFile{
			RelPath: join(c.GoPkgName + ".gno"),
			Content: grcSrc,
		})
		if g.opts.GenerateTests {
			out = append(out, OutputFile{
				RelPath: join(c.GoPkgName + "_test.gno"),
				Content: g.emitSmokeTest(c),
			})
		}
		return out, nil
	}

	// 2. Archivo principal <pkg>.gno (se emite antes de solrt_gen.gno para detectar si un hueco IA usa solrt)
	mainSrc, needsSolrt, err := g.emitContractSource(ctx, c)
	if err != nil {
		return nil, err
	}
	out = append(out, OutputFile{
		RelPath: join(c.GoPkgName + ".gno"),
		Content: mainSrc,
	})

	// 3. Runtime embebido solrt_gen.gno (si se necesita)
	if g.opts.EmbedRuntime && (needsSolrt || c.UsesSolrt || c.HasInitArgs || c.UsesReentrancyGuard) {
		rtSrc, err := buildEmbeddedSolrt(c.GoPkgName)
		if err != nil {
			return nil, err
		}
		out = append(out, OutputFile{
			RelPath: join("solrt_gen.gno"),
			Content: rtSrc,
		})
	}

	// 4. Test smoke <pkg>_test.gno
	if g.opts.GenerateTests {
		testSrc := g.emitSmokeTest(c)
		out = append(out, OutputFile{
			RelPath: join(c.GoPkgName + "_test.gno"),
			Content: testSrc,
		})
	}

	return out, nil
}

type emitCtx struct {
	ctx              context.Context
	g                *Generator
	c                *ir.Contract
	curFn            *ir.Function
	curFnSig         string
	rtPrefix         string // "" si EmbedRuntime, "solrt." si import externo
	stateMap         map[string]*ir.StateVar
	stateGoNames     map[string]bool
	funcMap          map[string]*ir.Function
	eventMap         map[string]*ir.EventDef
	enumMap          map[string]*ir.EnumDef
	structMap        map[string]*ir.StructDef
	structMapHelpers map[string]*ir.StructDef // structs almacenados en mappings
	localVars        map[string]ir.TypeRef
	localName        map[string]string // nombre Solidity -> nombre Go local en función actual
	storageStructPtr map[string]bool   // locales que son *Struct apuntando a storage
	storageSlicePtr  map[string]bool   // locales que son *[]T apuntando a storage
	unchecked        bool
}

func (g *Generator) emitContractSource(ctx context.Context, c *ir.Contract) ([]byte, bool, error) {
	rtPrefix := ""
	if !g.opts.EmbedRuntime {
		rtPrefix = "solrt."
	}

	ec := &emitCtx{
		ctx:              ctx,
		g:                g,
		c:                c,
		rtPrefix:         rtPrefix,
		stateMap:         make(map[string]*ir.StateVar, len(c.StateVars)),
		stateGoNames:     make(map[string]bool, len(c.StateVars)),
		funcMap:          make(map[string]*ir.Function, len(c.Functions)),
		eventMap:         make(map[string]*ir.EventDef, len(c.Events)),
		enumMap:          make(map[string]*ir.EnumDef, len(c.Enums)),
		structMap:        make(map[string]*ir.StructDef, len(c.Structs)),
		structMapHelpers: make(map[string]*ir.StructDef),
	}
	for _, sv := range c.StateVars {
		ec.stateMap[sv.Name] = sv
		ec.stateGoNames[sv.GoName] = true
	}
	for _, fn := range c.Functions {
		if fn.Name != "" {
			ec.funcMap[fn.Name] = fn
		}
	}
	for _, ev := range c.Events {
		ec.eventMap[ev.Name] = ev
	}
	for _, en := range c.Enums {
		ec.enumMap[en.Name] = en
	}
	for _, st := range c.Structs {
		ec.structMap[st.Name] = st
	}
	// Detectar structs que son valor de un mapping para generar helpers tipados (sin generics Go 1.18+)
	for _, sv := range c.StateVars {
		vt := innermostMapValue(sv.Type)
		if vt.Kind == "struct" {
			if st, ok := ec.structMap[vt.Name]; ok {
				ec.structMapHelpers[st.Name] = st
			}
		}
	}

	// Emitir primero el cuerpo del archivo para detectar con exactitud qué imports
	// se usan realmente (incluyendo los introducidos por bloques resueltos por IA).
	var bodyBuf bytes.Buffer

	// Enums
	for _, en := range c.Enums {
		fmt.Fprintf(&bodyBuf, "type %s uint8\n\nconst (\n", en.GoName)
		for i, m := range en.Members {
			if i == 0 {
				fmt.Fprintf(&bodyBuf, "\t%s_%s %s = iota\n", en.GoName, m, en.GoName)
			} else {
				fmt.Fprintf(&bodyBuf, "\t%s_%s\n", en.GoName, m)
			}
		}
		bodyBuf.WriteString(")\n\n")
	}

	// Structs y helpers de mapping tipados (Go 1.17)
	for _, st := range c.Structs {
		fmt.Fprintf(&bodyBuf, "type %s struct {\n", st.GoName)
		for _, f := range st.Fields {
			fmt.Fprintf(&bodyBuf, "\t%s %s\n", f.GoName, ec.gnoType(f.Type))
		}
		bodyBuf.WriteString("}\n\n")

		if _, needsHelpers := ec.structMapHelpers[st.Name]; needsHelpers {
			fmt.Fprintf(&bodyBuf, "func getOrInit%s(m *%sOrderedMap, key string) *%s {\n", st.GoName, rtPrefix, st.GoName)
			fmt.Fprintf(&bodyBuf, "\tv, ok := m.Get(key)\n")
			fmt.Fprintf(&bodyBuf, "\tif !ok || v == nil {\n")
			fmt.Fprintf(&bodyBuf, "\t\ts := &%s{}\n", st.GoName)
			fmt.Fprintf(&bodyBuf, "\t\tm.Set(key, s)\n")
			fmt.Fprintf(&bodyBuf, "\t\treturn s\n")
			fmt.Fprintf(&bodyBuf, "\t}\n")
			fmt.Fprintf(&bodyBuf, "\treturn v.(*%s)\n", st.GoName)
			fmt.Fprintf(&bodyBuf, "}\n\n")

			fmt.Fprintf(&bodyBuf, "func get%s(m *%sOrderedMap, key string) %s {\n", st.GoName, rtPrefix, st.GoName)
			fmt.Fprintf(&bodyBuf, "\tv, ok := m.Get(key)\n")
			fmt.Fprintf(&bodyBuf, "\tif !ok || v == nil {\n")
			fmt.Fprintf(&bodyBuf, "\t\treturn %s{}\n", st.GoName)
			fmt.Fprintf(&bodyBuf, "\t}\n")
			fmt.Fprintf(&bodyBuf, "\treturn *v.(*%s)\n", st.GoName)
			fmt.Fprintf(&bodyBuf, "}\n\n")
		}
	}

	// Variables de estado persistentes
	if len(c.StateVars) > 0 || c.HasInitArgs || c.UsesReentrancyGuard {
		bodyBuf.WriteString("var (\n")
		if c.HasInitArgs {
			bodyBuf.WriteString("\tdeployer address\n")
			bodyBuf.WriteString("\tinitialized bool\n")
		}
		if c.UsesReentrancyGuard {
			bodyBuf.WriteString("\treentrancyLocked bool\n")
		}
		for _, sv := range c.StateVars {
			switch {
			case sv.Type.Kind == "mapping":
				fmt.Fprintf(&bodyBuf, "\t%s = %sNewOrderedMap()\n", sv.GoName, rtPrefix)
			case sv.InitialValue != nil:
				initStr := ec.emitExprAs(sv.InitialValue, sv.Type)
				fmt.Fprintf(&bodyBuf, "\t%s %s = %s\n", sv.GoName, ec.gnoType(sv.Type), initStr)
			default:
				fmt.Fprintf(&bodyBuf, "\t%s %s\n", sv.GoName, ec.gnoType(sv.Type))
			}
		}
		bodyBuf.WriteString(")\n\n")
	}

	// Captura atómica del desplegador en el despliegue del Realm para impedir front-running en Init(...)
	if c.HasInitArgs {
		bodyBuf.WriteString("func init(cur realm) {\n")
		bodyBuf.WriteString("\tdeployer = cur.Previous().Address()\n")
		bodyBuf.WriteString("}\n\n")
	}

	// Funciones
	for _, fn := range c.Functions {
		ec.emitFunction(&bodyBuf, fn)
		bodyBuf.WriteString("\n")
	}

	// Render(path string) string opcional
	if g.opts.GenerateRender {
		if g.opts.Idiomatic {
			ec.emitIdiomaticRender(&bodyBuf)
		} else {
			fmt.Fprintf(&bodyBuf, "// Render expone una vista de solo lectura en gno.land.\n")
			fmt.Fprintf(&bodyBuf, "func Render(path string) string {\n")
			fmt.Fprintf(&bodyBuf, "\treturn \"# %s\\n\\nTranspilado con gnofy (gnofy.xyz).\\n\"\n", c.Name)
			fmt.Fprintf(&bodyBuf, "}\n")
		}
	}

	bodyStr := bodyBuf.String()
	needsChain := strings.Contains(bodyStr, "chain.")
	needsBanker := strings.Contains(bodyStr, "banker.")
	needsRuntime := strings.Contains(bodyStr, "runtime.")
	needsUnsafe := strings.Contains(bodyStr, "unsafe.")
	needsSolrt := detectSolrtUsage(bodyStr)

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by gnofy (Solidity -> Gno). Source: %s\n", c.SourcePath)
	fmt.Fprintf(&buf, "package %s\n\n", c.GoPkgName)

	var imports []string
	if needsChain {
		imports = append(imports, `"chain"`)
	}
	if needsBanker {
		imports = append(imports, `"chain/banker"`)
	}
	if needsRuntime {
		imports = append(imports, `"chain/runtime"`)
	}
	if needsUnsafe {
		imports = append(imports, `"chain/runtime/unsafe"`)
	}
	if !g.opts.EmbedRuntime && (needsSolrt || c.UsesSolrt || c.HasInitArgs || c.UsesReentrancyGuard) {
		imports = append(imports, fmt.Sprintf("solrt %q", g.opts.RuntimeImport))
	}
	if len(imports) > 0 {
		buf.WriteString("import (\n")
		for _, imp := range imports {
			fmt.Fprintf(&buf, "\t%s\n", imp)
		}
		buf.WriteString(")\n\n")
	}

	buf.WriteString(bodyStr)

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), needsSolrt, nil
	}
	return formatted, needsSolrt, nil
}

func detectSolrtUsage(body string) bool {
	for _, sym := range []string{
		"U256", "Require(", "Revert(", "Assert(",
		"SafeAddU", "SafeSubU", "SafeMulU", "SafeDivU", "SafeModU",
		"SafeAddI", "SafeSubI", "SafeMulI", "SafeDivI", "SafeModI", "SafeNegI",
		"CheckUnsignedBitsU64(", "CheckSignedBitsI64(", ".CheckBits(",
		"MulDiv(", "NewOrderedMap(", "KeyJoin(", "FormatU64(", "FormatI64(", "FormatBool(",
		"ERC20Token", "ResolveERC20(", "SafeERC20Transfer(", "SafeERC20TransferFrom(",
	} {
		if strings.Contains(body, sym) {
			return true
		}
	}
	return false
}

func (ec *emitCtx) emitFunction(buf *bytes.Buffer, fn *ir.Function) {
	ec.curFn = fn
	ec.localVars = make(map[string]ir.TypeRef)
	ec.localName = make(map[string]string)
	ec.storageStructPtr = make(map[string]bool)
	ec.storageSlicePtr = make(map[string]bool)

	// Firma
	var params []string
	if fn.IsCrossing {
		params = append(params, "cur realm")
	}
	for _, p := range fn.Params {
		ec.localVars[p.Name] = p.Type
		ec.localVars[p.GoName] = p.Type
		ec.localName[p.Name] = p.GoName
		params = append(params, fmt.Sprintf("%s %s", p.GoName, ec.gnoType(p.Type)))
	}

	var retParts []string
	namedReturns := false
	for _, r := range fn.Returns {
		if r.GoName != "" {
			namedReturns = true
			ec.localVars[r.Name] = r.Type
			ec.localVars[r.GoName] = r.Type
			ec.localName[r.Name] = r.GoName
		}
	}
	for _, r := range fn.Returns {
		if namedReturns {
			name := r.GoName
			if name == "" {
				name = "_"
			}
			retParts = append(retParts, fmt.Sprintf("%s %s", name, ec.gnoType(r.Type)))
		} else {
			retParts = append(retParts, ec.gnoType(r.Type))
		}
	}

	retSig := ""
	if len(retParts) == 1 && !namedReturns {
		retSig = " " + retParts[0]
	} else if len(retParts) > 0 {
		retSig = " (" + strings.Join(retParts, ", ") + ")"
	}

	ec.curFnSig = fmt.Sprintf("func %s(%s)%s", fn.GoName, strings.Join(params, ", "), retSig)
	fmt.Fprintf(buf, "%s {\n", ec.curFnSig)

	// Guard anti-front-running (fail-closed) + single-init para constructor parametrizado Init(...)
	if fn.IsInit && len(fn.Params) > 0 {
		fmt.Fprintf(buf, "\t%sRequire(deployer != address(\"\") && cur.Previous().Address() == deployer, \"%s: only deployer can initialize\")\n", ec.rtPrefix, ec.c.Name)
		fmt.Fprintf(buf, "\t%sRequire(!initialized, \"%s: already initialized\")\n", ec.rtPrefix, ec.c.Name)
		buf.WriteString("\tinitialized = true\n")
	}

	// Guard nonReentrant si aplica
	for _, mc := range fn.Modifiers {
		if mc.Name == "nonReentrant" {
			fmt.Fprintf(buf, "\t%sRequire(!reentrancyLocked, \"ReentrancyGuard: reentrant call\")\n", ec.rtPrefix)
			buf.WriteString("\treentrancyLocked = true\n")
			buf.WriteString("\tdefer func() { reentrancyLocked = false }()\n")
			break
		}
	}

	ec.emitStmts(buf, fn.Body, 1)

	if namedReturns && !endsWithReturn(fn.Body) {
		buf.WriteString("\treturn\n")
	}
	buf.WriteString("}\n")
}

func (ec *emitCtx) emitStmts(buf *bytes.Buffer, stmts []ir.Stmt, indent int) {
	pad := strings.Repeat("\t", indent)
	for _, s := range stmts {
		switch st := s.(type) {
		case *ir.BlockStmt:
			hasDecl := false
			for _, inner := range st.Stmts {
				if _, ok := inner.(*ir.VarDeclStmt); ok {
					hasDecl = true
					break
				}
			}
			if hasDecl {
				savedVars := make(map[string]ir.TypeRef, len(ec.localVars))
				for k, v := range ec.localVars {
					savedVars[k] = v
				}
				savedNames := make(map[string]string, len(ec.localName))
				for k, v := range ec.localName {
					savedNames[k] = v
				}
				fmt.Fprintf(buf, "%s{\n", pad)
				ec.emitStmts(buf, st.Stmts, indent+1)
				fmt.Fprintf(buf, "%s}\n", pad)
				ec.localVars = savedVars
				ec.localName = savedNames
			} else {
				ec.emitStmts(buf, st.Stmts, indent)
			}

		case *ir.UncheckedStmt:
			prev := ec.unchecked
			ec.unchecked = true
			ec.emitStmts(buf, st.Body, indent)
			ec.unchecked = prev

		case *ir.VarDeclStmt:
			ec.emitVarDecl(buf, st, pad)

		case *ir.AssignStmt:
			ec.emitAssign(buf, st, pad)

		case *ir.ExprStmt:
			// Casos especiales de mutación de arrays: arr.push(x) y arr.pop()
			if ce, ok := st.Expr.(*ir.CallExpr); ok {
				if mem, ok := ce.Callee.(*ir.MemberExpr); ok {
					if mem.Member == "push" && len(ce.Args) == 1 {
						arrExpr, isPtr, elemType := ec.resolveArrayTarget(mem.Object)
						argStr := ec.emitExprAs(ce.Args[0], elemType)
						if isPtr {
							fmt.Fprintf(buf, "%s*%s = append(*%s, %s)\n", pad, arrExpr, arrExpr, argStr)
						} else {
							fmt.Fprintf(buf, "%s%s = append(%s, %s)\n", pad, arrExpr, arrExpr, argStr)
						}
						continue
					}
					if mem.Member == "pop" && len(ce.Args) == 0 {
						arrExpr, isPtr, _ := ec.resolveArrayTarget(mem.Object)
						if isPtr {
							fmt.Fprintf(buf, "%s*%s = (*%s)[:len(*%s)-1]\n", pad, arrExpr, arrExpr, arrExpr)
						} else {
							fmt.Fprintf(buf, "%s%s = %s[:len(%s)-1]\n", pad, arrExpr, arrExpr, arrExpr)
						}
						continue
					}
				}
			}
			fmt.Fprintf(buf, "%s%s\n", pad, ec.emitExpr(st.Expr))

		case *ir.IfStmt:
			fmt.Fprintf(buf, "%sif %s {\n", pad, ec.emitExpr(st.Cond))
			ec.emitStmts(buf, st.Then, indent+1)
			if len(st.Else) > 0 {
				fmt.Fprintf(buf, "%s} else {\n", pad)
				ec.emitStmts(buf, st.Else, indent+1)
			}
			fmt.Fprintf(buf, "%s}\n", pad)

		case *ir.ForStmt:
			initStr := strings.TrimSpace(ec.emitInlineStmt(st.Init))
			condStr := ec.emitExpr(st.Cond)
			postStr := strings.TrimSpace(ec.emitInlineStmt(st.Post))
			fmt.Fprintf(buf, "%sfor %s; %s; %s {\n", pad, initStr, condStr, postStr)
			ec.emitStmts(buf, st.Body, indent+1)
			fmt.Fprintf(buf, "%s}\n", pad)

		case *ir.WhileStmt:
			fmt.Fprintf(buf, "%sfor %s {\n", pad, ec.emitExpr(st.Cond))
			ec.emitStmts(buf, st.Body, indent+1)
			fmt.Fprintf(buf, "%s}\n", pad)

		case *ir.ReturnStmt:
			if st.Value == nil {
				fmt.Fprintf(buf, "%sreturn\n", pad)
			} else {
				retStr := ec.emitExpr(st.Value)
				// Si retornamos una referencia *Struct de storage pero la firma devuelve valor Struct, desreferenciar
				if id, ok := st.Value.(*ir.IdentExpr); ok && ec.storageStructPtr[id.Name] {
					retStr = "*" + retStr
				}
				fmt.Fprintf(buf, "%sreturn %s\n", pad, retStr)
			}

		case *ir.EmitStmt:
			ec.emitEvent(buf, st, pad)

		case *ir.RevertStmt:
			fmt.Fprintf(buf, "%s%sRevert(%q)\n", pad, ec.rtPrefix, st.ErrorName)

		case *ir.BreakStmt:
			fmt.Fprintf(buf, "%sbreak\n", pad)

		case *ir.ContinueStmt:
			fmt.Fprintf(buf, "%scontinue\n", pad)

		case *ir.HoleStmt:
			res, _ := ec.g.opts.Resolver.Resolve(ec.ctx, ec.buildHoleRequest(st.Hole))
			annotated := ai.FormatAnnotated(st.Hole.ID, res)
			for _, line := range strings.Split(annotated, "\n") {
				fmt.Fprintf(buf, "%s%s\n", pad, line)
			}
		}
	}
}

func (ec *emitCtx) buildHoleRequest(h *ir.Hole) ai.HoleRequest {
	var stateSummary strings.Builder
	for _, sv := range ec.c.StateVars {
		fmt.Fprintf(&stateSummary, "- %s %s\n", sv.GoName, ec.gnoType(sv.Type))
	}
	return ai.HoleRequest{
		Hole:              h,
		ContractName:      ec.c.Name,
		FunctionSignature: ec.curFnSig,
		IsCrossing:        ec.curFn != nil && ec.curFn.IsCrossing,
		StateVarsSummary:  strings.TrimSpace(stateSummary.String()),
		AllowedImports:    []string{"chain", "chain/banker", "chain/runtime", "chain/runtime/unsafe"},
	}
}

func (ec *emitCtx) emitVarDecl(buf *bytes.Buffer, st *ir.VarDeclStmt, pad string) {
	if len(st.Vars) == 1 {
		v := st.Vars[0]
		goName := ec.bindLocalName(v.Name, v.Type)

		// Referencia storage a struct en mapping: `Investor storage inv = investments[msg.sender];`
		if st.IsStorage && v.Type.Kind == "struct" && st.Init != nil {
			if mapVar, keyExpr, valType, ok := ec.unwrapMappingAccess(st.Init); ok && valType.Kind == "struct" {
				if structDef, exists := ec.structMap[valType.Name]; exists {
					ec.storageStructPtr[v.Name] = true
					ec.storageStructPtr[goName] = true
					fmt.Fprintf(buf, "%s%s := getOrInit%s(%s, %s)\n", pad, goName, structDef.GoName, mapVar.GoName, keyExpr)
					return
				}
			}
		}

		// Referencia storage a slice de estado: `uint128[] storage seller_otcs = otcs;`
		if st.IsStorage && v.Type.Kind == "array" && st.Init != nil {
			ec.storageSlicePtr[v.Name] = true
			ec.storageSlicePtr[goName] = true
			fmt.Fprintf(buf, "%s%s := &%s\n", pad, goName, ec.emitExpr(st.Init))
			return
		}

		if st.Init != nil {
			fmt.Fprintf(buf, "%s%s := %s\n", pad, goName, ec.emitExprAs(st.Init, v.Type))
		} else {
			fmt.Fprintf(buf, "%svar %s %s\n", pad, goName, ec.gnoType(v.Type))
		}
		return
	}

	var names []string
	for _, v := range st.Vars {
		gn := ec.bindLocalName(v.Name, v.Type)
		names = append(names, gn)
	}
	if st.Init != nil {
		fmt.Fprintf(buf, "%s%s := %s\n", pad, strings.Join(names, ", "), ec.emitExpr(st.Init))
	}
}

var gnoReservedLocals = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"realm": true, "cur": true,
	"append": true, "cap": true, "close": true, "complex": true, "copy": true,
	"delete": true, "imag": true, "len": true, "make": true, "new": true,
	"panic": true, "print": true, "println": true, "real": true, "recover": true,
	"revive": true, "cross": true, "address": true, "any": true, "bool": true,
	"byte": true, "error": true, "rune": true, "string": true, "true": true,
	"false": true, "nil": true, "iota": true,
}

func (ec *emitCtx) bindLocalName(solName string, typ ir.TypeRef) string {
	if solName == "_" {
		return "_"
	}
	goName := unexportCamel(solName)
	if ec.stateGoNames[goName] || gnoReservedLocals[goName] {
		goName += "Local"
	}
	ec.localVars[solName] = typ
	ec.localVars[goName] = typ
	ec.localName[solName] = goName
	return goName
}

func (ec *emitCtx) emitInlineStmt(s ir.Stmt) string {
	if s == nil {
		return ""
	}
	var b bytes.Buffer
	ec.emitStmts(&b, []ir.Stmt{s}, 0)
	return strings.TrimSpace(b.String())
}

func (ec *emitCtx) emitAssign(buf *bytes.Buffer, st *ir.AssignStmt, pad string) {
	// Caso especial: asignación a mapping (simple o anidado)
	if mapVar, keyExpr, valType, ok := ec.unwrapMappingAccess(st.LHS); ok {
		// Mapping de struct: m[k] = Struct(...) -> m.Set(k, &Struct{...})
		if valType.Kind == "struct" {
			rhsStr := ec.emitExprAs(st.RHS, valType)
			if !strings.HasPrefix(rhsStr, "&") {
				rhsStr = "&" + rhsStr
			}
			fmt.Fprintf(buf, "%s%s.Set(%s, %s)\n", pad, mapVar.GoName, keyExpr, rhsStr)
			return
		}
		getter, setter := ec.mapAccessorNames(valType)
		var rhsStr string
		if st.Op == "=" {
			rhsStr = ec.emitExprAs(st.RHS, valType)
		} else {
			curVal := fmt.Sprintf("%s.%s(%s)", mapVar.GoName, getter, keyExpr)
			rhsVal := ec.emitExprAs(st.RHS, valType)
			rhsStr = ec.applyCompoundOp(st.Op, curVal, rhsVal, valType)
		}
		fmt.Fprintf(buf, "%s%s.%s(%s, %s)\n", pad, mapVar.GoName, setter, keyExpr, rhsStr)
		return
	}

	lhsType := ec.resolveExprType(st.LHS)
	lhsStr := ec.emitExpr(st.LHS)
	if st.Op == "=" {
		fmt.Fprintf(buf, "%s%s = %s\n", pad, lhsStr, ec.emitExprAs(st.RHS, lhsType))
		return
	}
	rhsStr := ec.emitExprAs(st.RHS, lhsType)
	combined := ec.applyCompoundOp(st.Op, lhsStr, rhsStr, lhsType)
	fmt.Fprintf(buf, "%s%s = %s\n", pad, lhsStr, combined)
}

func (ec *emitCtx) applyCompoundOp(op, lhs, rhs string, typ ir.TypeRef) string {
	baseOp := strings.TrimSuffix(op, "=")
	return ec.emitArith(baseOp, lhs, rhs, typ)
}

func (ec *emitCtx) emitEvent(buf *bytes.Buffer, st *ir.EmitStmt, pad string) {
	var evDef *ir.EventDef
	if d, ok := ec.eventMap[st.EventName]; ok {
		evDef = d
	}

	parts := []string{fmt.Sprintf("%q", st.EventName)}
	for i, arg := range st.Args {
		key := fmt.Sprintf("arg%d", i)
		if evDef != nil && i < len(evDef.Params) && evDef.Params[i].Name != "" {
			key = evDef.Params[i].Name
		}
		parts = append(parts, fmt.Sprintf("%q", key), ec.emitAsString(arg))
	}
	fmt.Fprintf(buf, "%schain.Emit(%s)\n", pad, strings.Join(parts, ", "))
}

func (ec *emitCtx) emitExpr(e ir.Expr) string {
	if e == nil {
		return ""
	}
	switch ex := e.(type) {
	case *ir.LiteralExpr:
		return ec.emitLiteral(ex, ex.Type)

	case *ir.IdentExpr:
		if ln, ok := ec.localName[ex.Name]; ok {
			return ln
		}
		if sv, ok := ec.stateMap[ex.Name]; ok {
			return sv.GoName
		}
		if fn, ok := ec.funcMap[ex.Name]; ok {
			return fn.GoName
		}
		return unexportCamel(ex.Name)

	case *ir.MemberExpr:
		// msg.sender, tx.origin, block.number
		if id, ok := ex.Object.(*ir.IdentExpr); ok {
			if id.Name == "msg" && ex.Member == "sender" {
				if ec.curFn != nil && ec.curFn.IsCrossing {
					return "cur.Previous().Address()"
				}
				return "unsafe.PreviousRealm().Address()"
			}
			if id.Name == "tx" && ex.Member == "origin" {
				return "unsafe.PreviousRealm().Address()"
			}
			if id.Name == "block" && ex.Member == "number" {
				return "uint64(runtime.ChainHeight())"
			}
			if en, ok := ec.enumMap[id.Name]; ok {
				return en.GoName + "_" + ex.Member
			}
			if id.Name == "this" {
				if fn, ok := ec.funcMap[ex.Member]; ok {
					return fn.GoName
				}
				return exportCamel(ex.Member)
			}
		}

		// address.balance -> U256FromU64(uint64(banker.NewReadonlyBanker().GetCoin(addr, "ugnot")))
		if ex.Member == "balance" && ec.resolveExprType(ex.Object).Kind == "address" {
			return fmt.Sprintf("%sU256FromU64(uint64(banker.NewReadonlyBanker().GetCoin(%s, \"ugnot\")))",
				ec.rtPrefix, ec.emitExpr(ex.Object))
		}

		// array.length
		if ex.Member == "length" {
			if id, ok := ex.Object.(*ir.IdentExpr); ok && ec.storageSlicePtr[id.Name] {
				return fmt.Sprintf("uint64(len(*%s))", ec.emitExpr(ex.Object))
			}
			return fmt.Sprintf("uint64(len(%s))", ec.emitExpr(ex.Object))
		}

		// Acceso a campo de struct dentro de un mapping: investments[investor].amount_raw
		if mapVar, keyExpr, valType, ok := ec.unwrapMappingAccess(ex.Object); ok && valType.Kind == "struct" {
			if structDef, exists := ec.structMap[valType.Name]; exists {
				return fmt.Sprintf("getOrInit%s(%s, %s).%s", structDef.GoName, mapVar.GoName, keyExpr, exportCamel(ex.Member))
			}
		}

		return fmt.Sprintf("%s.%s", ec.emitExpr(ex.Object), exportCamel(ex.Member))

	case *ir.IndexExpr:
		if mapVar, keyExpr, valType, ok := ec.unwrapMappingAccess(ex); ok {
			if valType.Kind == "struct" {
				if structDef, exists := ec.structMap[valType.Name]; exists {
					return fmt.Sprintf("get%s(%s, %s)", structDef.GoName, mapVar.GoName, keyExpr)
				}
			}
			getter, _ := ec.mapAccessorNames(valType)
			return fmt.Sprintf("%s.%s(%s)", mapVar.GoName, getter, keyExpr)
		}
		baseStr := ec.emitExpr(ex.Base)
		if id, ok := ex.Base.(*ir.IdentExpr); ok && ec.storageSlicePtr[id.Name] {
			baseStr = "(*" + baseStr + ")"
		}
		idxStr := ec.emitExpr(ex.Index)
		if ec.resolveExprType(ex.Index).IsU256() {
			idxStr += ".Uint64()"
		}
		return fmt.Sprintf("%s[%s]", baseStr, idxStr)

	case *ir.BinaryExpr:
		lt := ec.resolveExprType(ex.Left)
		rt := ec.resolveExprType(ex.Right)
		opType := lt
		if lt.Kind == "int_const" && rt.Kind != "int_const" {
			opType = rt
		} else if rt.IsU256() {
			opType = rt
		}
		left := ec.emitExprAs(ex.Left, opType)
		right := ec.emitExprAs(ex.Right, opType)

		switch ex.Op {
		case "+", "-", "*", "/", "%", "**":
			return ec.emitArith(ex.Op, left, right, opType)
		case "==", "!=", "<", "<=", ">", ">=":
			if opType.IsU256() {
				return ec.emitU256Cmp(ex.Op, left, right)
			}
			return fmt.Sprintf("(%s %s %s)", left, ex.Op, right)
		default:
			return fmt.Sprintf("(%s %s %s)", left, ex.Op, right)
		}

	case *ir.UnaryExpr:
		// Literal negativo (p.ej. -1, -128): mantener como literal sin SafeNeg
		if ex.Op == "-" {
			if _, isLit := ex.Operand.(*ir.LiteralExpr); isLit {
				return "-" + ec.emitExpr(ex.Operand)
			}
		}
		operand := ec.emitExpr(ex.Operand)
		opType := ec.resolveExprType(ex.Operand)
		if ex.Op == "++" {
			if ec.unchecked && (opType.IsU64OrSmaller() || opType.IsSigned64OrSmaller()) {
				return fmt.Sprintf("%s++", operand)
			}
			one := ec.emitLiteral(&ir.LiteralExpr{Kind: "number", Value: "1"}, opType)
			return fmt.Sprintf("%s = %s", operand, ec.emitArith("+", operand, one, opType))
		}
		if ex.Op == "--" {
			if ec.unchecked && (opType.IsU64OrSmaller() || opType.IsSigned64OrSmaller()) {
				return fmt.Sprintf("%s--", operand)
			}
			one := ec.emitLiteral(&ir.LiteralExpr{Kind: "number", Value: "1"}, opType)
			return fmt.Sprintf("%s = %s", operand, ec.emitArith("-", operand, one, opType))
		}
		if ex.Op == "-" && !ec.unchecked && opType.IsSigned64OrSmaller() {
			return ec.emitSignedNeg(operand, opType)
		}
		return fmt.Sprintf("%s%s", ex.Op, operand)

	case *ir.CallExpr:
		return ec.emitCall(ex)

	case *ir.TupleExpr:
		var parts []string
		for _, comp := range ex.Components {
			parts = append(parts, ec.emitExpr(comp))
		}
		return strings.Join(parts, ", ")

	case *ir.HoleExpr:
		res, _ := ec.g.opts.Resolver.Resolve(ec.ctx, ec.buildHoleRequest(ex.Hole))
		return res.GnoCode

	default:
		return "/* unsupported expr */"
	}
}

func (ec *emitCtx) emitCall(ex *ir.CallExpr) string {
	// Struct constructor call: OTC(msg.sender, amount, price_wei) -> OTC{Seller: ..., AmountBaseWei: ..., PriceWei: ...}
	if ex.IsStructCtor {
		if st, ok := ec.structMap[ex.Type.Name]; ok {
			var fields []string
			for i, arg := range ex.Args {
				if i < len(st.Fields) {
					fields = append(fields, fmt.Sprintf("%s: %s", st.Fields[i].GoName, ec.emitExprAs(arg, st.Fields[i].Type)))
				}
			}
			return fmt.Sprintf("%s{%s}", st.GoName, strings.Join(fields, ", "))
		}
	}

	// Type casts: address(0), address(this), IERC20(addr), uint256(x), etc.
	if ex.IsTypeCast && len(ex.Args) == 1 {
		if ex.CastTarget.Kind == "contract" && ex.CastTarget.Name == "IERC20" {
			return fmt.Sprintf("%sResolveERC20(%s)", ec.rtPrefix, ec.emitExpr(ex.Args[0]))
		}
		if ex.CastTarget.Kind == "address" {
			if lit, ok := ex.Args[0].(*ir.LiteralExpr); ok && lit.Value == "0" {
				return `address("")`
			}
			if id, ok := ex.Args[0].(*ir.IdentExpr); ok && id.Name == "this" {
				if ec.curFn != nil && ec.curFn.IsCrossing {
					return "cur.Address()"
				}
				return "unsafe.CurrentRealm().Address()"
			}
			return fmt.Sprintf("address(%s)", ec.emitExpr(ex.Args[0]))
		}
		if ex.CastTarget.IsU256() {
			return ec.emitExprAs(ex.Args[0], ex.CastTarget)
		}
		return fmt.Sprintf("%s(%s)", ec.gnoType(ex.CastTarget), ec.emitExpr(ex.Args[0]))
	}

	// Builtins Solidity: require, assert, revert
	if id, ok := ex.Callee.(*ir.IdentExpr); ok {
		switch id.Name {
		case "require":
			if len(ex.Args) == 1 {
				return fmt.Sprintf("%sRequire(%s, \"\")", ec.rtPrefix, ec.emitExpr(ex.Args[0]))
			}
			if len(ex.Args) >= 2 {
				return fmt.Sprintf("%sRequire(%s, %s)", ec.rtPrefix, ec.emitExpr(ex.Args[0]), ec.emitExpr(ex.Args[1]))
			}
		case "assert":
			if len(ex.Args) >= 1 {
				return fmt.Sprintf("%sAssert(%s)", ec.rtPrefix, ec.emitExpr(ex.Args[0]))
			}
		case "revert":
			if len(ex.Args) == 0 {
				return fmt.Sprintf("%sRevert(\"\")", ec.rtPrefix)
			}
			return fmt.Sprintf("%sRevert(%s)", ec.rtPrefix, ec.emitExpr(ex.Args[0]))
		}
	}

	// Librerías estándar conocidas (PRBMath.mulDiv, SafeERC20.safeTransfer/safeTransferFrom)
	if mem, ok := ex.Callee.(*ir.MemberExpr); ok {
		if id, ok := mem.Object.(*ir.IdentExpr); ok {
			u256 := ir.TypeRef{Kind: "uint", BitSize: 256}
			if id.Name == "PRBMath" && mem.Member == "mulDiv" && len(ex.Args) == 3 {
				return fmt.Sprintf("%sMulDiv(%s, %s, %s)",
					ec.rtPrefix,
					ec.emitExprAs(ex.Args[0], u256),
					ec.emitExprAs(ex.Args[1], u256),
					ec.emitExprAs(ex.Args[2], u256),
				)
			}
			if id.Name == "SafeERC20" && mem.Member == "safeTransfer" && len(ex.Args) == 3 {
				return fmt.Sprintf("%sSafeERC20Transfer(%s, %s, %s)",
					ec.rtPrefix,
					ec.emitExpr(ex.Args[0]),
					ec.emitExpr(ex.Args[1]),
					ec.emitExprAs(ex.Args[2], u256),
				)
			}
			if id.Name == "SafeERC20" && mem.Member == "safeTransferFrom" && len(ex.Args) == 4 {
				return fmt.Sprintf("%sSafeERC20TransferFrom(%s, %s, %s, %s)",
					ec.rtPrefix,
					ec.emitExpr(ex.Args[0]),
					ec.emitExpr(ex.Args[1]),
					ec.emitExpr(ex.Args[2]),
					ec.emitExprAs(ex.Args[3], u256),
				)
			}
		}
	}

	// Llamada a función del propio contrato (directa o vía this.foo())
	var calleeStr string
	var targetFn *ir.Function
	if id, ok := ex.Callee.(*ir.IdentExpr); ok {
		if fn, exists := ec.funcMap[id.Name]; exists {
			targetFn = fn
			calleeStr = fn.GoName
		} else {
			calleeStr = ec.emitExpr(ex.Callee)
		}
	} else if mem, ok := ex.Callee.(*ir.MemberExpr); ok {
		if id, ok := mem.Object.(*ir.IdentExpr); ok && id.Name == "this" {
			if fn, exists := ec.funcMap[mem.Member]; exists {
				targetFn = fn
				calleeStr = fn.GoName
			} else {
				calleeStr = ec.emitExpr(ex.Callee)
			}
		} else {
			calleeStr = ec.emitExpr(ex.Callee)
		}
	} else {
		calleeStr = ec.emitExpr(ex.Callee)
	}

	var args []string
	if targetFn != nil && targetFn.IsCrossing {
		args = append(args, "cur")
	}
	for i, a := range ex.Args {
		if targetFn != nil && i < len(targetFn.Params) {
			args = append(args, ec.emitExprAs(a, targetFn.Params[i].Type))
		} else {
			args = append(args, ec.emitExpr(a))
		}
	}
	return fmt.Sprintf("%s(%s)", calleeStr, strings.Join(args, ", "))
}

func (ec *emitCtx) resolveArrayTarget(e ir.Expr) (exprStr string, isPtr bool, elemType ir.TypeRef) {
	t := ec.resolveExprType(e)
	if t.ElemType != nil {
		elemType = *t.ElemType
	}
	exprStr = ec.emitExpr(e)
	if id, ok := e.(*ir.IdentExpr); ok && ec.storageSlicePtr[id.Name] {
		isPtr = true
	}
	return exprStr, isPtr, elemType
}

func (ec *emitCtx) emitExprAs(e ir.Expr, target ir.TypeRef) string {
	if lit, ok := e.(*ir.LiteralExpr); ok {
		return ec.emitLiteral(lit, target)
	}
	if un, ok := e.(*ir.UnaryExpr); ok && un.Op == "-" {
		if lit, isLit := un.Operand.(*ir.LiteralExpr); isLit && lit.Kind == "number" {
			return "-" + lit.Value
		}
	}
	curType := ec.resolveExprType(e)
	raw := ec.emitExpr(e)

	if target.IsU256() && !curType.IsU256() && curType.Kind == "uint" {
		if strings.HasPrefix(raw, "uint64(") {
			return fmt.Sprintf("%sU256FromU64(%s)", ec.rtPrefix, raw)
		}
		return fmt.Sprintf("%sU256FromU64(uint64(%s))", ec.rtPrefix, raw)
	}
	if target.IsU64OrSmaller() && curType.IsU64OrSmaller() && ec.gnoType(target) != ec.gnoType(curType) {
		return fmt.Sprintf("%s(%s)", ec.gnoType(target), raw)
	}
	if target.IsSigned64OrSmaller() && curType.IsSigned64OrSmaller() && ec.gnoType(target) != ec.gnoType(curType) {
		return fmt.Sprintf("%s(%s)", ec.gnoType(target), raw)
	}
	return raw
}

func (ec *emitCtx) emitLiteral(lit *ir.LiteralExpr, target ir.TypeRef) string {
	switch lit.Kind {
	case "string":
		return fmt.Sprintf("%q", lit.Value)
	case "bool":
		return lit.Value
	case "address":
		return fmt.Sprintf("address(%q)", lit.Value)
	case "number":
		clean := strings.ReplaceAll(lit.Value, "_", "")
		if target.IsU256() {
			if v, err := strconv.ParseUint(clean, 10, 64); err == nil {
				return fmt.Sprintf("%sU256FromU64(%d)", ec.rtPrefix, v)
			}
			return fmt.Sprintf("%sU256FromString(%q)", ec.rtPrefix, clean)
		}
		return clean
	default:
		return lit.Value
	}
}

func (ec *emitCtx) emitArith(op, left, right string, typ ir.TypeRef) string {
	if typ.IsU256() {
		var expr string
		switch op {
		case "+":
			if ec.unchecked {
				expr = fmt.Sprintf("%s.UncheckedAdd(%s)", left, right)
			} else {
				expr = fmt.Sprintf("%s.Add(%s)", left, right)
			}
		case "-":
			if ec.unchecked {
				expr = fmt.Sprintf("%s.UncheckedSub(%s)", left, right)
			} else {
				expr = fmt.Sprintf("%s.Sub(%s)", left, right)
			}
		case "*":
			if ec.unchecked {
				expr = fmt.Sprintf("%s.UncheckedMul(%s)", left, right)
			} else {
				expr = fmt.Sprintf("%s.Mul(%s)", left, right)
			}
		case "/":
			expr = fmt.Sprintf("%s.Div(%s)", left, right)
		case "%":
			expr = fmt.Sprintf("%s.Mod(%s)", left, right)
		case "**":
			expr = fmt.Sprintf("%s.Pow(%s.Uint64())", left, right)
		default:
			return fmt.Sprintf("(%s %s %s)", left, op, right)
		}
		// Comprobar límite exacto de N bits para uint72..uint248 (p.ej. uint96, uint128) fuera de unchecked
		if !ec.unchecked && typ.BitSize > 64 && typ.BitSize < 256 {
			return fmt.Sprintf("%s.CheckBits(%d)", expr, typ.BitSize)
		}
		return expr
	}

	if typ.IsU64OrSmaller() && !ec.unchecked {
		opName := arithHelperOpName(op)
		if opName != "" {
			switch typ.BitSize {
			case 8:
				return fmt.Sprintf("%sSafe%sU8(%s, %s)", ec.rtPrefix, opName, left, right)
			case 16:
				return fmt.Sprintf("%sSafe%sU16(%s, %s)", ec.rtPrefix, opName, left, right)
			case 32:
				return fmt.Sprintf("%sSafe%sU32(%s, %s)", ec.rtPrefix, opName, left, right)
			case 64:
				return fmt.Sprintf("%sSafe%sU64(%s, %s)", ec.rtPrefix, opName, left, right)
			default:
				// Anchos intermedios (uint24, uint40, uint48, uint56): usar uint64 + límite exacto N bits
				inner := fmt.Sprintf("%sSafe%sU64(uint64(%s), uint64(%s))", ec.rtPrefix, opName, left, right)
				checked := fmt.Sprintf("%sCheckUnsignedBitsU64(%s, %d)", ec.rtPrefix, inner, typ.BitSize)
				gnoT := ec.gnoType(typ)
				if gnoT != "uint64" {
					return fmt.Sprintf("%s(%s)", gnoT, checked)
				}
				return checked
			}
		}
	}

	if typ.IsSigned64OrSmaller() && !ec.unchecked {
		opName := arithHelperOpName(op)
		if opName != "" {
			switch typ.BitSize {
			case 8:
				return fmt.Sprintf("%sSafe%sI8(%s, %s)", ec.rtPrefix, opName, left, right)
			case 16:
				return fmt.Sprintf("%sSafe%sI16(%s, %s)", ec.rtPrefix, opName, left, right)
			case 32:
				return fmt.Sprintf("%sSafe%sI32(%s, %s)", ec.rtPrefix, opName, left, right)
			case 64:
				return fmt.Sprintf("%sSafe%sI64(%s, %s)", ec.rtPrefix, opName, left, right)
			default:
				inner := fmt.Sprintf("%sSafe%sI64(int64(%s), int64(%s))", ec.rtPrefix, opName, left, right)
				checked := fmt.Sprintf("%sCheckSignedBitsI64(%s, %d)", ec.rtPrefix, inner, typ.BitSize)
				gnoT := ec.gnoType(typ)
				if gnoT != "int64" {
					return fmt.Sprintf("%s(%s)", gnoT, checked)
				}
				return checked
			}
		}
	}

	return fmt.Sprintf("(%s %s %s)", left, op, right)
}

func arithHelperOpName(op string) string {
	switch op {
	case "+":
		return "Add"
	case "-":
		return "Sub"
	case "*":
		return "Mul"
	case "/":
		return "Div"
	case "%":
		return "Mod"
	default:
		return ""
	}
}

func (ec *emitCtx) emitSignedNeg(operand string, typ ir.TypeRef) string {
	switch typ.BitSize {
	case 8:
		return fmt.Sprintf("%sSafeNegI8(%s)", ec.rtPrefix, operand)
	case 16:
		return fmt.Sprintf("%sSafeNegI16(%s)", ec.rtPrefix, operand)
	case 32:
		return fmt.Sprintf("%sSafeNegI32(%s)", ec.rtPrefix, operand)
	case 64:
		return fmt.Sprintf("%sSafeNegI64(%s)", ec.rtPrefix, operand)
	default:
		inner := fmt.Sprintf("%sCheckSignedBitsI64(%sSafeNegI64(int64(%s)), %d)", ec.rtPrefix, ec.rtPrefix, operand, typ.BitSize)
		if gnoT := ec.gnoType(typ); gnoT != "int64" {
			return fmt.Sprintf("%s(%s)", gnoT, inner)
		}
		return inner
	}
}

func (ec *emitCtx) emitU256Cmp(op, left, right string) string {
	switch op {
	case "==":
		return fmt.Sprintf("%s.Eq(%s)", left, right)
	case "!=":
		return fmt.Sprintf("%s.Neq(%s)", left, right)
	case "<":
		return fmt.Sprintf("%s.Lt(%s)", left, right)
	case "<=":
		return fmt.Sprintf("%s.Lte(%s)", left, right)
	case ">":
		return fmt.Sprintf("%s.Gt(%s)", left, right)
	case ">=":
		return fmt.Sprintf("%s.Gte(%s)", left, right)
	default:
		return fmt.Sprintf("(%s %s %s)", left, op, right)
	}
}

func (ec *emitCtx) emitAsString(e ir.Expr) string {
	t := ec.resolveExprType(e)
	raw := ec.emitExpr(e)
	switch {
	case t.Kind == "string":
		return raw
	case t.Kind == "address":
		return fmt.Sprintf("string(%s)", raw)
	case t.IsU256():
		return fmt.Sprintf("%s.String()", raw)
	case t.Kind == "uint":
		return fmt.Sprintf("%sFormatU64(uint64(%s))", ec.rtPrefix, raw)
	case t.Kind == "int":
		return fmt.Sprintf("%sFormatI64(int64(%s))", ec.rtPrefix, raw)
	case t.Kind == "bool":
		return fmt.Sprintf("%sFormatBool(%s)", ec.rtPrefix, raw)
	default:
		return fmt.Sprintf("%sFormatU64(uint64(%s))", ec.rtPrefix, raw)
	}
}

func (ec *emitCtx) unwrapMappingAccess(e ir.Expr) (mapVar *ir.StateVar, keyExpr string, valType ir.TypeRef, ok bool) {
	var indices []ir.Expr
	cur := e
	for {
		ix, isIdx := cur.(*ir.IndexExpr)
		if !isIdx {
			break
		}
		indices = append([]ir.Expr{ix.Index}, indices...)
		cur = ix.Base
	}
	if len(indices) == 0 {
		return nil, "", ir.TypeRef{}, false
	}
	id, isIdent := cur.(*ir.IdentExpr)
	if !isIdent {
		return nil, "", ir.TypeRef{}, false
	}
	sv, exists := ec.stateMap[id.Name]
	if !exists || sv.Type.Kind != "mapping" {
		return nil, "", ir.TypeRef{}, false
	}

	curType := sv.Type
	var keyParts []string
	for _, idxExpr := range indices {
		if curType.Kind != "mapping" || curType.ValueType == nil {
			return nil, "", ir.TypeRef{}, false
		}
		keyParts = append(keyParts, ec.emitAsString(idxExpr))
		curType = *curType.ValueType
	}

	if len(keyParts) == 1 {
		keyExpr = keyParts[0]
	} else {
		keyExpr = fmt.Sprintf("%sKeyJoin(%s)", ec.rtPrefix, strings.Join(keyParts, ", "))
	}
	return sv, keyExpr, curType, true
}

func (ec *emitCtx) mapAccessorNames(valType ir.TypeRef) (getter, setter string) {
	switch {
	case valType.IsU256():
		return "GetU256", "SetU256"
	case valType.Kind == "uint":
		switch {
		case valType.BitSize <= 8:
			return "GetU8", "SetU8"
		case valType.BitSize <= 16:
			return "GetU16", "SetU16"
		case valType.BitSize <= 32:
			return "GetU32", "SetU32"
		default:
			return "GetU64", "SetU64"
		}
	case valType.IsSigned64OrSmaller():
		switch {
		case valType.BitSize <= 8:
			return "GetI8", "SetI8"
		case valType.BitSize <= 16:
			return "GetI16", "SetI16"
		case valType.BitSize <= 32:
			return "GetI32", "SetI32"
		default:
			return "GetI64", "SetI64"
		}
	case valType.Kind == "bool":
		return "GetBool", "SetBool"
	case valType.Kind == "string":
		return "GetString", "SetString"
	default:
		return "Get", "Set"
	}
}

func (ec *emitCtx) resolveExprType(e ir.Expr) ir.TypeRef {
	if e == nil {
		return ir.TypeRef{Kind: "unknown"}
	}
	switch x := e.(type) {
	case *ir.IdentExpr:
		if t, ok := ec.localVars[x.Name]; ok {
			return t
		}
		if sv, ok := ec.stateMap[x.Name]; ok {
			return sv.Type
		}
	case *ir.MemberExpr:
		if x.Member == "length" {
			return ir.TypeRef{Kind: "uint", BitSize: 64}
		}
	case *ir.BinaryExpr:
		switch x.Op {
		case "+", "-", "*", "/", "%", "**":
			lt := ec.resolveExprType(x.Left)
			rt := ec.resolveExprType(x.Right)
			if lt.Kind == "int_const" && rt.Kind != "int_const" {
				return rt
			}
			if rt.IsU256() {
				return rt
			}
			return lt
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return ir.TypeRef{Kind: "bool"}
		}
	}
	if _, _, vt, ok := ec.unwrapMappingAccess(e); ok {
		return vt
	}
	return e.ExprType()
}

func (ec *emitCtx) gnoType(t ir.TypeRef) string {
	switch {
	case t.Kind == "bool":
		return "bool"
	case t.Kind == "string":
		return "string"
	case t.Kind == "address":
		return "address"
	case t.Kind == "bytes" && t.BitSize > 0:
		return fmt.Sprintf("[%d]byte", t.BitSize)
	case t.Kind == "bytes":
		return "[]byte"
	case t.IsU256():
		return ec.rtPrefix + "U256"
	case t.Kind == "uint":
		switch {
		case t.BitSize <= 8:
			return "uint8"
		case t.BitSize <= 16:
			return "uint16"
		case t.BitSize <= 32:
			return "uint32"
		default:
			return "uint64"
		}
	case t.IsWideSigned():
		return "any /* UNSUPPORTED_WIDE_SIGNED_INT */"
	case t.Kind == "int":
		switch {
		case t.BitSize <= 8:
			return "int8"
		case t.BitSize <= 16:
			return "int16"
		case t.BitSize <= 32:
			return "int32"
		default:
			return "int64"
		}
	case t.Kind == "array" && t.ElemType != nil:
		return "[]" + ec.gnoType(*t.ElemType)
	case t.Kind == "mapping":
		return "*" + ec.rtPrefix + "OrderedMap"
	case t.Kind == "enum":
		if en, ok := ec.enumMap[t.Name]; ok {
			return en.GoName
		}
		return t.Name
	case t.Kind == "struct":
		if st, ok := ec.structMap[t.Name]; ok {
			return st.GoName
		}
		return t.Name
	case t.Kind == "contract" && t.Name == "IERC20":
		return ec.rtPrefix + "ERC20Token"
	default:
		return "any"
	}
}

func (g *Generator) emitSmokeTest(c *ir.Contract) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by gnofy. Smoke test básico para %s.\n", c.Name)
	fmt.Fprintf(&buf, "package %s\n\n", c.GoPkgName)
	buf.WriteString("import \"testing\"\n\n")
	fmt.Fprintf(&buf, "func Test%sCompiles(t *testing.T) {\n", c.Name)
	if g.opts.GenerateRender {
		buf.WriteString("\tif Render(\"\") == \"\" {\n")
		buf.WriteString("\t\tt.Fatalf(\"Render returned empty output\")\n")
		buf.WriteString("\t}\n")
	} else {
		buf.WriteString("\t_ = t\n")
	}
	buf.WriteString("}\n")
	return buf.Bytes()
}

func buildEmbeddedSolrt(pkgName string) ([]byte, error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "// Code generated by gnofy (embedded solrt runtime, Go 1.17 / Gno).\n")
	fmt.Fprintf(&buf, "package %s\n\n", pkgName)
	buf.WriteString("import \"strconv\"\n\n")

	order := []string{"p/solrt/solrt.gno", "p/solrt/signed.gno", "p/solrt/u256.gno", "p/solrt/map.gno", "p/solrt/erc20.gno"}
	for _, name := range order {
		raw, err := fs.ReadFile(gnoruntime.SolrtFS, name)
		if err != nil {
			return nil, fmt.Errorf("leyendo runtime embebido %s: %w", name, err)
		}
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "package ") || trimmed == `import "strconv"` {
				continue
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
		buf.WriteByte('\n')
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return buf.Bytes(), nil
	}
	return formatted, nil
}

func innermostMapValue(t ir.TypeRef) ir.TypeRef {
	cur := t
	for cur.Kind == "mapping" && cur.ValueType != nil {
		cur = *cur.ValueType
	}
	return cur
}

func exportCamel(name string) string {
	trimmed := strings.TrimLeft(name, "_")
	if trimmed == "" {
		return "Exported"
	}
	parts := strings.Split(trimmed, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	return b.String()
}

func unexportCamel(name string) string {
	trimmed := strings.TrimLeft(name, "_")
	if trimmed == "" {
		return "v"
	}
	parts := strings.Split(trimmed, "_")
	var b strings.Builder
	first := true
	for _, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		if first {
			runes[0] = unicode.ToLower(runes[0])
			first = false
		} else {
			runes[0] = unicode.ToUpper(runes[0])
		}
		b.WriteString(string(runes))
	}
	return b.String()
}

func endsWithReturn(stmts []ir.Stmt) bool {
	if len(stmts) == 0 {
		return false
	}
	_, ok := stmts[len(stmts)-1].(*ir.ReturnStmt)
	return ok
}
