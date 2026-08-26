// stripcmt prints each Go file's AST with all comments discarded, so two
// revisions that differ only in comments produce byte-identical output.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	root := os.Args[1]
	var files []string
	filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	out := os.Stdout
	for _, p := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			fmt.Fprintln(os.Stderr, "PARSE ERROR:", p, err)
			os.Exit(1)
		}
		f.Comments = nil
		ast.SortImports(fset, f)
		rel, _ := filepath.Rel(root, p)
		fmt.Fprintln(out, "==== "+rel)
		printer.Fprint(out, fset, f)
		fmt.Fprintln(out)
	}
}
