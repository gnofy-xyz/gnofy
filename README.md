# gnofy

**gnofy** es una suite de herramientas escrita en **Go 1.27** para analizar, transpilar de forma determinista y verificar la migración de smart contracts escritos en **Solidity** al ecosistema **gno.land (Gno)**.

## Filosofía

1. **Nativo, no emulado:** genera Realms y Packages nativos de Gno (legibles, auditables y componibles on-chain), en lugar de emular una EVM sobre GnoVM.
2. **Determinismo primero:** el núcleo del transpilador consume el AST resuelto por `solc` (`--standard-json`) y aplica pases deterministas hacia código estrictamente compatible con **Gno (especificación Go 1.17 + stdlib de Gno)**.
3. **IA quirúrgica opcional (`--ai=assist`):** los constructos sin equivalencia 1:1 (ej. bloques `assembly`/Yul o patrones complejos) se aíslan como nodos `Hole` en la IR. Por defecto (`--ai=off`) se emiten stubs seguros `TODO`; con `--ai=assist` un modelo ligero resuelve únicamente esos huecos, los valida con `gno test` y fija el resultado en `gnofy.lock` para preservar reproducibilidad.

## Requisitos de entorno

| Herramienta | Propósito | Versión recomendada |
| :--- | :--- | :--- |
| **Go** | Compilar y ejecutar `gnofy` (host) | Go `1.27+` |
| **solc** | Parseo y resolución de tipos/herencia de Solidity | `0.8.x` |
| **gno** | Type-checking, lint y ejecución de `gno test` sobre la salida | Última versión de `gnolang/gno` |

> **Nota sobre versiones:** `gnofy` se desarrolla en **Go 1.27**, pero el código Gno que genera cumple estrictamente la especificación **Go 1.17** (sin *generics*, sin `min`/`max`/`clear`, sin goroutines/canales) junto con las extensiones propias de Gno (`crossing` / `cur realm`, `chain/runtime`, `chain/banker`, `gnomod.toml`).

## Comandos previstos del CLI

```bash
# 1. Auditar compatibilidad y reporte de migrabilidad de contratos Solidity
gnofy analyze ./contracts

# 2. Transpilar de forma 100% determinista (sin IA)
gnofy convert ./contracts/Counter.sol -o ./out/r/counter

# 3. Transpilar habilitando resolución quirúrgica de huecos con modelo local/API
gnofy convert ./contracts/Vault.sol -o ./out/r/vault --ai=assist --ai-endpoint=http://localhost:11434/v1

# 4. Verificar el Realm generado ejecutando gno test
gnofy verify ./out/r/counter
```

## Estructura del repositorio

```text
gnofy/
├── cmd/gnofy/            # Punto de entrada CLI (analyze, convert, verify)
├── internal/
│   ├── frontend/solc/        # Invocación de solc y modelos tipados del AST JSON
│   ├── analyzer/             # Auditoría de migrabilidad, scoring y detección de patrones
│   ├── ir/                   # Representación Intermedia (ContractIR, FuncIR, StmtIR, HoleIR)
│   ├── passes/               # Pases deterministas (C3 flattening, modifiers, tipos, estándares)
│   ├── ai/                   # Interfaz HoleResolver, cliente OpenAI-compat y lockfile
│   ├── codegen/gno/          # Emisor Gno (Go 1.17), gnomod.toml, Render() y *_test.gno
│   └── verify/               # Runner de verificación contra el toolchain `gno`
├── gno/p/solrt/              # Paquete runtime en Gno puro (u256 checked, avl helpers, require)
├── testdata/contracts/       # Corpus de contratos Solidity + golden outputs Gno
└── docs/
    ├── ARCHITECTURE.md       # Arquitectura completa del pipeline y contrato del motor IA
    ├── SOLIDITY_TO_GNO.md    # Especificación detallada de mapeos semánticos Solidity ↔ Gno
    └── ROADMAP.md            # Plan de hitos incrementales (v0.1 → v0.4)
```

## Documentación

- [Arquitectura del sistema](docs/ARCHITECTURE.md)
- [Especificación de mapeo Solidity → Gno](docs/SOLIDITY_TO_GNO.md)
- [Roadmap de desarrollo](docs/ROADMAP.md)
