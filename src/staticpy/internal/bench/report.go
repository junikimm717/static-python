package bench

import (
	"fmt"
	"html"
	"strings"
)

// SuiteReport is everything the markdown and HTML reports share, so the
// two cannot drift on environment, pins, or identity the way they used to.
type SuiteReport struct {
	Baseline   string
	Order      []string
	Rows       []Row
	Geomean    map[string]float64
	Machine    Machine
	Protocol   int
	Pins       Pins
	Identities []Identity
	Skipped    int
}

func (r SuiteReport) pins() Pins {
	return r.Pins.withDefaults()
}

func (r SuiteReport) protocol() int {
	if r.Protocol == 0 {
		return Protocol
	}
	return r.Protocol
}

// EnvMarkdown is the provenance block shared with the micro suite, which
// has no pyperformance rows but still has to say where the numbers came from.
func EnvMarkdown(m Machine, protocol int, pins Pins) string {
	if protocol == 0 {
		protocol = Protocol
	}
	pins = pins.withDefaults()
	var b strings.Builder
	b.WriteString("## Environment\n\n")
	fmt.Fprintf(&b, "- protocol: %d\n", protocol)
	fmt.Fprintf(&b, "- suite: pyperformance %s, pyperf %s\n", pins.Pyperformance, pins.Pyperf)
	fmt.Fprintf(&b, "- kernel: %s\n", dash(m.Kernel))
	fmt.Fprintf(&b, "- cpu: %s\n", dash(m.CPUModel))
	if m.Memory != "" {
		if m.MemoryAvailable != "" {
			fmt.Fprintf(&b, "- memory: %s (%s available)\n", m.Memory, m.MemoryAvailable)
		} else {
			fmt.Fprintf(&b, "- memory: %s\n", m.Memory)
		}
	} else {
		b.WriteString("- memory: unknown\n")
	}
	fmt.Fprintf(&b, "- logical cores: %d\n", m.Cores)
	fmt.Fprintf(&b, "- caches: L1d %s / L1i %s / L2 %s / L3 %s\n",
		dash(m.CacheL1d), dash(m.CacheL1i), dash(m.CacheL2), dash(m.CacheL3))
	if m.Topology != "" {
		fmt.Fprintf(&b, "- topology: %s\n", m.Topology)
	}
	if m.Affinity != "" {
		fmt.Fprintf(&b, "- affinity: %s\n", m.Affinity)
	}
	if m.Container {
		b.WriteString("- container: yes\n")
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func (r SuiteReport) Markdown() string {
	var b strings.Builder
	b.WriteString("# pyperformance comparison\n\n")
	b.WriteString(EnvMarkdown(r.Machine, r.protocol(), r.pins()))
	fmt.Fprintf(&b, "- baseline: %s\n", r.Baseline)
	fmt.Fprintf(&b, "- rows: %d\n", len(r.Rows))
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "- skipped: %d (see skipped.json)\n", r.Skipped)
	} else {
		b.WriteString("- skipped: 0 (see skipped.json)\n")
	}
	b.WriteString("\n")

	if len(r.Identities) > 0 {
		b.WriteString("## Interpreters\n\n")
		b.WriteString("| label | sha256 | linkage | size |\n|---|---|---|---:|\n")
		for _, id := range r.Identities {
			fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n",
				id.Label, shortSHA(id.SHA256), dash(id.Linkage), formatBytes(id.Size))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "| benchmark |")
	for _, a := range r.Order {
		fmt.Fprintf(&b, " %s |", a)
	}
	fmt.Fprintf(&b, "\n|---|")
	for range r.Order {
		fmt.Fprintf(&b, "---:|")
	}
	b.WriteString("\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "| %s |", row.Benchmark)
		for _, a := range r.Order {
			if v, ok := row.Ratio[a]; ok {
				fmt.Fprintf(&b, " %.2fx |", v)
			} else {
				b.WriteString(" - |")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\nGeomean vs baseline (>1 is faster):\n\n")
	for _, a := range r.Order {
		if a == r.Baseline {
			continue
		}
		fmt.Fprintf(&b, "- %s: %.3fx\n", a, r.Geomean[a])
	}
	return b.String()
}

func (r SuiteReport) HTML() string {
	var b strings.Builder
	pins := r.pins()
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>pyperformance comparison</title>
<style>
body{font:14px/1.45 system-ui,sans-serif;max-width:960px;margin:2rem auto;padding:0 1rem;color:#1a202c}
h1,h2{font-weight:600}
table{border-collapse:collapse;width:100%;margin:1rem 0}
th,td{border:1px solid #cbd5e0;padding:.35rem .6rem;text-align:right}
th:first-child,td:first-child{text-align:left}
.env{background:#f7fafc;padding:.75rem 1rem;border-radius:6px}
.env dt{font-weight:600;float:left;clear:left;width:9rem}
.env dd{margin-left:9.5rem}
.skip{color:#744210}
svg{max-width:100%}
</style>
</head>
<body>
`)
	b.WriteString("<h1>pyperformance comparison</h1>\n")
	b.WriteString(r.envHTML())
	b.WriteString("<h2>Geomean vs " + html.EscapeString(r.Baseline) + "</h2>\n")
	b.WriteString(r.geomeanSVG())
	if r.Skipped > 0 {
		fmt.Fprintf(&b, `<p class="skip">%d benchmarks omitted from the table; see <code>skipped.json</code>.</p>`+"\n", r.Skipped)
	} else {
		b.WriteString("<p>No benchmarks skipped; see <code>skipped.json</code>.</p>\n")
	}
	b.WriteString("<h2>Per-benchmark ratio vs " + html.EscapeString(r.Baseline) + "</h2>\n")
	b.WriteString(r.ratioTableHTML())
	if len(r.Identities) > 0 {
		b.WriteString("<h2>Interpreters</h2>\n")
		b.WriteString(r.identityTableHTML())
	}
	fmt.Fprintf(&b, "<p>protocol %d · pyperformance %s · pyperf %s</p>\n",
		r.protocol(), html.EscapeString(pins.Pyperformance), html.EscapeString(pins.Pyperf))
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

func (r SuiteReport) envHTML() string {
	m := r.Machine
	pins := r.pins()
	mem := "unknown"
	if m.Memory != "" {
		mem = m.Memory
		if m.MemoryAvailable != "" {
			mem += " (" + m.MemoryAvailable + " available)"
		}
	}
	row := func(k, v string) string {
		return "<dt>" + html.EscapeString(k) + "</dt><dd>" + html.EscapeString(v) + "</dd>\n"
	}
	var b strings.Builder
	b.WriteString(`<dl class="env">` + "\n")
	b.WriteString(row("protocol", fmt.Sprintf("%d", r.protocol())))
	b.WriteString(row("suite", "pyperformance "+pins.Pyperformance+", pyperf "+pins.Pyperf))
	b.WriteString(row("kernel", dash(m.Kernel)))
	b.WriteString(row("cpu", dash(m.CPUModel)))
	b.WriteString(row("memory", mem))
	b.WriteString(row("logical cores", fmt.Sprintf("%d", m.Cores)))
	b.WriteString(row("caches", fmt.Sprintf("L1d %s / L1i %s / L2 %s / L3 %s",
		dash(m.CacheL1d), dash(m.CacheL1i), dash(m.CacheL2), dash(m.CacheL3))))
	if m.Topology != "" {
		b.WriteString(row("topology", m.Topology))
	}
	if m.Affinity != "" {
		b.WriteString(row("affinity", m.Affinity))
	}
	if m.Container {
		b.WriteString(row("container", "yes"))
	}
	b.WriteString(row("baseline", r.Baseline))
	b.WriteString("</dl>\n")
	return b.String()
}

func (r SuiteReport) geomeanSVG() string {
	const (
		labelW = 180
		barMax = 360
		barH   = 22
		gap    = 10
		left   = 16
		top    = 16
	)
	n := len(r.Order)
	if n == 0 {
		return "<p>no arms</p>\n"
	}
	maxV := 1.0
	vals := make([]float64, n)
	for i, a := range r.Order {
		v := 1.0
		if a != r.Baseline {
			v = r.Geomean[a]
			if v <= 0 {
				v = 0
			}
		}
		vals[i] = v
		if v > maxV {
			maxV = v
		}
	}
	width := left + labelW + barMax + 80
	height := top + n*(barH+gap) + 8
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="geomean vs %s">`+"\n",
		width, height, width, height, html.EscapeString(r.Baseline))
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/>`+"\n", width, height)
	for i, a := range r.Order {
		y := top + i*(barH+gap)
		label := html.EscapeString(a)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="end" font-size="13" fill="#1a202c">%s</text>`+"\n",
			left+labelW-8, y+barH-6, label)
		bw := 0.0
		if maxV > 0 {
			bw = vals[i] / maxV * barMax
		}
		fill := "#2b6cb0"
		if a == r.Baseline {
			fill = "#4a5568"
		}
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%.1f" height="%d" fill="%s" rx="3"/>`+"\n",
			left+labelW, y, bw, barH, fill)
		fmt.Fprintf(&b, `<text x="%.1f" y="%d" font-size="12" fill="#1a202c">%.3fx</text>`+"\n",
			float64(left+labelW)+bw+8, y+barH-6, vals[i])
	}
	b.WriteString("</svg>\n")
	return b.String()
}

func (r SuiteReport) ratioTableHTML() string {
	var b strings.Builder
	b.WriteString("<table>\n<thead><tr><th>benchmark</th>")
	for _, a := range r.Order {
		fmt.Fprintf(&b, "<th>%s</th>", html.EscapeString(a))
	}
	b.WriteString("</tr></thead>\n<tbody>\n")
	for _, row := range r.Rows {
		fmt.Fprintf(&b, "<tr><td>%s</td>", html.EscapeString(row.Benchmark))
		for _, a := range r.Order {
			if v, ok := row.Ratio[a]; ok {
				fmt.Fprintf(&b, "<td>%.2fx</td>", v)
			} else {
				b.WriteString("<td>-</td>")
			}
		}
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table>\n")
	return b.String()
}

func (r SuiteReport) identityTableHTML() string {
	var b strings.Builder
	b.WriteString("<table>\n<thead><tr><th>label</th><th>sha256</th><th>linkage</th><th>size</th></tr></thead>\n<tbody>\n")
	for _, id := range r.Identities {
		fmt.Fprintf(&b, "<tr><td>%s</td><td><code>%s</code></td><td>%s</td><td>%s</td></tr>\n",
			html.EscapeString(id.Label),
			html.EscapeString(shortSHA(id.SHA256)),
			html.EscapeString(dash(id.Linkage)),
			html.EscapeString(formatBytes(id.Size)))
	}
	b.WriteString("</tbody></table>\n")
	return b.String()
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "-"
	}
	return s
}
