package gen

import (
	"strings"
)

// BuiltinName flattens a dotted module name into the identifier makesetup and
// config.c use.
func BuiltinName(dotted string) string {
	if !strings.Contains(dotted, ".") {
		return dotted
	}
	return "_" + strings.ReplaceAll(dotted, ".", "_")
}
