package terraform

import (
	"strings"

	"github.com/magicdrive/ark/internal/symbol"
)

// Resource modes: the three block types that declare a TYPE.NAME instance.
const (
	ModeManaged   = "managed"   // resource "TYPE" "NAME"
	ModeData      = "data"      // data "TYPE" "NAME"
	ModeEphemeral = "ephemeral" // ephemeral "TYPE" "NAME"
)

// ResourceAddress splits the address this provider gives a resource-like
// declaration (symbol.Symbol.Name, see the package doc) into its mode, type
// and name: TYPE.NAME and resource.TYPE.NAME (a type that reads as a reference
// root) are managed resources, ephemeral.TYPE.NAME ephemeral ones — both
// KindResource — and data.TYPE.NAME data sources (KindDataSource). A
// Terraform identifier contains no dot, so the parts are the dot-separated
// labels. ok is false for any other symbol.
func ResourceAddress(kind symbol.SymbolKind, address string) (mode, typ, name string, ok bool) {
	parts := strings.Split(address, ".")
	switch {
	case kind == symbol.KindDataSource && len(parts) == 3 && parts[0] == "data":
		mode, parts = ModeData, parts[1:]
	case kind == symbol.KindResource && len(parts) == 3 && parts[0] == "ephemeral":
		mode, parts = ModeEphemeral, parts[1:]
	case kind == symbol.KindResource && len(parts) == 3 && parts[0] == "resource":
		mode, parts = ModeManaged, parts[1:]
	case kind == symbol.KindResource && len(parts) == 2:
		mode = ModeManaged
	default:
		return "", "", "", false
	}
	if parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return mode, parts[0], parts[1], true
}
