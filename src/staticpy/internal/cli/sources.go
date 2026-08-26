package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/junikimm717/static-python/src/staticpy/internal/config"
	"github.com/junikimm717/static-python/src/staticpy/internal/core"
	"github.com/junikimm717/static-python/src/staticpy/internal/sources"
)

var cmdSources = &command{
	Name:     "sources",
	Short:    "list, download and re-check the pinned upstream tarballs",
	Synopsis: "staticpy sources [list|fetch|verify] [NAME]...",
	Long: `Every input to the build is pinned by sha256 in sources.toml, which is embedded
in this binary. A tarball is used only once its hash matches the pin; one that
does not match is deleted rather than left for the next run to reuse.

  list    the pins, and whether each is already in dist/src (the default)
  fetch   download whatever is missing, verifying as it lands
  verify  re-hash what is on disk against the pins, ignoring the .done markers

Because the checksums are part of every job's content key, changing a pin
invalidates exactly the artifacts that depend on it and nothing else.

` + "`fetch`" + ` is worth running on its own before a long build on a flaky link, and is
what makes ` + "`--offline`" + ` usable afterwards: with --offline nothing but dist/src is
consulted, so a build on a disconnected machine fails immediately and clearly
instead of stalling on a dial timeout.

Name one or more sources to restrict the operation to them.`,
	Run: runSources,
}

func runSources(g *Global, args []string) error {
	action := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "list", "fetch", "verify":
			action, args = args[0], args[1:]
		default:
			return usagef("unknown subcommand %q: want list, fetch or verify", args[0])
		}
	}
	fs := g.flagSet("sources")
	if err := parse(fs, args); err != nil {
		return finish("sources", err)
	}
	cfg, err := g.load()
	if err != nil {
		return err
	}
	e, done, err := g.newEnv(cfg, action == "fetch")
	if err != nil {
		return err
	}
	defer done()

	names, err := selectSources(cfg, fs.Args())
	if err != nil {
		return err
	}
	switch action {
	case "fetch":
		return fetchSources(g, e, cfg, names)
	case "verify":
		return verifySources(g, e, cfg, names)
	}
	return listSources(g, e, cfg, names)
}

func selectSources(cfg *config.Config, want []string) ([]string, error) {
	if len(want) == 0 {
		return sortedKeys(cfg.Sources), nil
	}
	for _, n := range want {
		if _, ok := cfg.Sources[n]; !ok {
			return nil, fmt.Errorf("unknown source %q.\nPinned sources: %s", n, strings.Join(sortedKeys(cfg.Sources), ", "))
		}
	}
	return want, nil
}

func listSources(g *Global, e *core.Env, cfg *config.Config, names []string) error {
	type row struct {
		Name    string   `json:"name"`
		Version string   `json:"version"`
		File    string   `json:"file"`
		SHA256  string   `json:"sha256"`
		Path    string   `json:"path"`
		Cached  bool     `json:"cached"`
		URLs    []string `json:"urls"`
	}
	var rows []row
	for _, n := range names {
		s := cfg.Sources[n]
		rows = append(rows, row{s.Name, s.Version, s.File, s.SHA256, sources.Path(e, s), sources.Fetched(e, s), s.URLs})
	}
	if g.JSON {
		return emitJSON(map[string]any{"cache": sources.CacheDir(e), "sources": rows})
	}
	t := newTable("NAME", "VERSION", "SHA256", "CACHED")
	missing := 0
	for _, r := range rows {
		mark := green("yes")
		if !r.Cached {
			mark = dim("no")
			missing++
		}
		t.add(r.Name, r.Version, dim(shortKey(r.SHA256)), mark)
	}
	t.render(os.Stdout)
	fmt.Printf("\n%s\n", dim("cache: "+sources.CacheDir(e)))
	if missing > 0 {
		fmt.Printf("%s\n", dim(fmt.Sprintf("%d not downloaded yet; `staticpy sources fetch` gets them", missing)))
	}
	return nil
}

func fetchSources(g *Global, e *core.Env, cfg *config.Config, names []string) error {
	ctx, stop := signalContext()
	defer stop()
	fetched := 0
	for _, n := range names {
		s := cfg.Sources[n]
		if sources.Fetched(e, s) {
			continue
		}
		e.Log.Info("fetching", "source", s.Name, "version", s.Version, "file", s.File)
		if _, err := sources.Fetch(ctx, e, s); err != nil {
			return err
		}
		fetched++
	}
	if fetched == 0 {
		fmt.Printf("%s every pinned source is already in %s\n", green("ok:"), sources.CacheDir(e))
		return nil
	}
	fmt.Printf("%s %d source%s into %s\n", green("fetched"), fetched, plural(fetched), sources.CacheDir(e))
	return nil
}

// verifySources re-hashes the bytes on disk rather than trusting the .done
// marker a previous run wrote. Trusting the marker would make this command a
// tautology.
func verifySources(g *Global, e *core.Env, cfg *config.Config, names []string) error {
	type row struct {
		Name   string `json:"name"`
		Path   string `json:"path"`
		Status string `json:"status"`
		Actual string `json:"actual,omitempty"`
	}
	var rows []row
	bad := 0
	for _, n := range names {
		s := cfg.Sources[n]
		path := sources.Path(e, s)
		r := row{Name: s.Name, Path: path}
		sum, err := hashFile(path)
		switch {
		case os.IsNotExist(err):
			r.Status = "absent"
		case err != nil:
			r.Status = "unreadable"
			r.Actual = err.Error()
			bad++
		case sum == s.SHA256:
			r.Status = "ok"
		default:
			r.Status = "mismatch"
			r.Actual = sum
			bad++
		}
		rows = append(rows, r)
	}
	if g.JSON {
		return emitJSON(map[string]any{"cache": sources.CacheDir(e), "sources": rows, "bad": bad})
	}
	t := newTable("NAME", "STATUS", "DETAIL")
	for _, r := range rows {
		switch r.Status {
		case "ok":
			t.add(r.Name, green("ok"), dim(r.Path))
		case "absent":
			t.add(r.Name, dim("absent"), dim(r.Path))
		default:
			t.add(r.Name, red(r.Status), r.Actual)
		}
	}
	t.render(os.Stdout)
	if bad > 0 {
		return fmt.Errorf("%d source%s on disk do not match their pin. Delete them from %s and re-fetch; staticpy will not build from bytes it cannot verify",
			bad, plural(bad), sources.CacheDir(e))
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
