package solc

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Remapping mapea un prefijo de import Solidity (p.ej. "@openzeppelin/contracts/")
// a una ruta relativa o absoluta en disco (p.ej. "lib/openzeppelin-contracts/contracts/").
type Remapping struct {
	Prefix string
	Target string
}

var (
	tomlRemappingsRe = regexp.MustCompile(`(?s)remappings\s*=\s*\[(.*?)\]`)
	tomlLibsRe       = regexp.MustCompile(`(?s)libs\s*=\s*\[(.*?)\]`)
	quotedStringRe   = regexp.MustCompile(`["']([^"']+)["']`)
)

// DiscoverFoundryConfig busca foundry.toml y remappings.txt subiendo desde startDir
// y extrae remappings e include paths (libs) sin dependencias externas.
func DiscoverFoundryConfig(startDir string) (remappings []Remapping, includePaths []string) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, nil
	}

	root := findProjectRoot(dir)
	if root == "" {
		return nil, nil
	}

	// 1. remappings.txt (prioridad más alta en Foundry)
	if raw, err := os.ReadFile(filepath.Join(root, "remappings.txt")); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if rm, ok := parseRemappingLine(root, line); ok {
				remappings = append(remappings, rm)
			}
		}
	}

	// 2. foundry.toml (remappings = [...] y libs = [...])
	if raw, err := os.ReadFile(filepath.Join(root, "foundry.toml")); err == nil {
		text := string(raw)
		if m := tomlRemappingsRe.FindStringSubmatch(text); len(m) > 1 {
			for _, q := range quotedStringRe.FindAllStringSubmatch(m[1], -1) {
				if rm, ok := parseRemappingLine(root, q[1]); ok {
					remappings = append(remappings, rm)
				}
			}
		}
		if m := tomlLibsRe.FindStringSubmatch(text); len(m) > 1 {
			for _, q := range quotedStringRe.FindAllStringSubmatch(m[1], -1) {
				includePaths = append(includePaths, filepath.Join(root, filepath.FromSlash(q[1])))
			}
		}
		// Directorio lib/ por defecto en Foundry si existe
		defaultLib := filepath.Join(root, "lib")
		if info, err := os.Stat(defaultLib); err == nil && info.IsDir() {
			includePaths = append(includePaths, defaultLib)
		}
	}

	return remappings, includePaths
}

func findProjectRoot(startDir string) string {
	cur := startDir
	for {
		if fileExists(filepath.Join(cur, "foundry.toml")) || fileExists(filepath.Join(cur, "remappings.txt")) {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func parseRemappingLine(root, line string) (Remapping, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return Remapping{}, false
	}
	// Quitar posible contexto "context:prefix=target"
	if colon := strings.IndexByte(line, ':'); colon >= 0 {
		eq := strings.IndexByte(line, '=')
		if eq > colon {
			line = line[colon+1:]
		}
	}
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return Remapping{}, false
	}
	prefix := strings.TrimSpace(line[:eq])
	target := strings.TrimSpace(line[eq+1:])
	if prefix == "" || target == "" {
		return Remapping{}, false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, filepath.FromSlash(target))
	}
	return Remapping{Prefix: prefix, Target: target}, true
}

func applyRemappings(imp string, remappings []Remapping) (string, bool) {
	for _, rm := range remappings {
		if strings.HasPrefix(imp, rm.Prefix) {
			rest := strings.TrimPrefix(imp, rm.Prefix)
			return filepath.Join(rm.Target, filepath.FromSlash(rest)), true
		}
	}
	return "", false
}
