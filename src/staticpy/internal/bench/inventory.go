package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/core"
)

// The script asks the interpreter what it can actually import, not what
// the pin file said we would install. A --pyperformance directory or a
// failed pin would otherwise leave the manifest claiming 1.14.0 while
// the run used something else.
const inventoryScript = `
import json, sys
try:
    from importlib.metadata import distributions
except ImportError:
    from importlib_metadata import distributions
pkgs = {}
for d in distributions():
    name = None
    try:
        name = d.metadata["Name"]
    except Exception:
        name = getattr(d, "name", None)
    if name:
        pkgs[str(name)] = d.version
print(json.dumps(pkgs, sort_keys=True))
`

// InventoryPackages asks python which distributions it can import.
func InventoryPackages(ctx context.Context, x Exec, python string) (map[string]string, error) {
	out, err := x.Output(ctx, core.Cmd{
		Args: []string{python, "-c", strings.TrimSpace(inventoryScript)},
		Name: "inventory-packages",
	})
	if err != nil {
		return nil, fmt.Errorf("package inventory: %w", err)
	}
	var pkgs map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &pkgs); err != nil {
		return nil, fmt.Errorf("package inventory: not JSON: %w\n%s", err, strings.TrimSpace(out))
	}
	if pkgs == nil {
		pkgs = map[string]string{}
	}
	return pkgs, nil
}
