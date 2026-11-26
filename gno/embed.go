// Package gnoruntime expone los archivos fuente Gno de solrt embebidos en el binario gnofy.
package gnoruntime

import "embed"

// SolrtFS contiene los archivos fuente del runtime solrt en Gno puro (Go 1.17).
//
//go:embed p/solrt/solrt.gno p/solrt/signed.gno p/solrt/u256.gno p/solrt/map.gno p/solrt/erc20.gno
var SolrtFS embed.FS
