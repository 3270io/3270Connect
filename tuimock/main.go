// Command tuimock renders candidate TUI header/summary treatments so they can
// be reviewed before any of it lands in the real renderer. Not part of the
// build — delete this directory once a design is picked.
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
	"github.com/muesli/termenv"
)

// Palette lifted from templates/static/css/dashboard.css and
// docs/stylesheets/3270-theme.css so the TUI reads as the same surface.
const (
	cAccent  = "#4effb3" // --accent  phosphor green
	cAccent2 = "#7cf9d0"
	cText    = "#e6fff5" // --text
	cText2   = "#9fe6c8" // --text-2
	cText3   = "#5f9e86" // --text-3
	cLine    = "#1e5a44" // --line     (alpha flattened onto --bg)
	cLineHi  = "#2f7a5c" // --line-strong
	cInfo    = "#5ad2ff"
	cWarn    = "#f7c36b"
	cDanger  = "#ff6f82"
	cViolet  = "#b98cff"
	cInk     = "#02130a" // --accent-ink
)

func fg(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
func chip(fgHex, bgHex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fgHex)).Background(lipgloss.Color(bgHex)).Bold(true)
}

var (
	sAccent  = fg(cAccent)
	sText    = fg(cText)
	sText2   = fg(cText2)
	sText3   = fg(cText3)
	sLine    = fg(cLine)
	sLineHi  = fg(cLineHi)
	sWarn    = fg(cWarn)
	sDanger  = fg(cDanger)
	sInfo    = fg(cInfo)
	sViolet  = fg(cViolet)
	sEyebrow = lipgloss.NewStyle().Foreground(lipgloss.Color(cAccent)).Bold(true)
)

const W = 80 // outer width, incl. borders — 80 cols, same as the screens it drives

// ---------------------------------------------------------------- helpers --

func pad(s string, w int) string {
	n := w - lipgloss.Width(s)
	if n < 0 {
		n = 0
	}
	return s + strings.Repeat(" ", n)
}

func lpad(s string, w int) string {
	n := w - lipgloss.Width(s)
	if n < 0 {
		n = 0
	}
	return strings.Repeat(" ", n) + s
}

func rule(w int) string { return strings.Repeat("─", w) }

// hexRGB parses #rrggbb.
func hexRGB(h string) (int, int, int) {
	r, _ := strconv.ParseInt(h[1:3], 16, 32)
	g, _ := strconv.ParseInt(h[3:5], 16, 32)
	b, _ := strconv.ParseInt(h[5:7], 16, 32)
	return int(r), int(g), int(b)
}

func mix(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	return fmt.Sprintf("#%02x%02x%02x",
		int(float64(ar)+(float64(br)-float64(ar))*t),
		int(float64(ag)+(float64(bg)-float64(ag))*t),
		int(float64(ab)+(float64(bb)-float64(ab))*t),
	)
}

// gradient paints a line of text left-to-right across two colours, leaving
// spaces unpainted so the escape sequences stay cheap.
func gradient(s, from, to string) string {
	runes := []rune(s)
	var b strings.Builder
	span := float64(len(runes) - 1)
	if span <= 0 {
		span = 1
	}
	for i, r := range runes {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(fg(mix(from, to, float64(i)/span)).Render(string(r)))
	}
	return b.String()
}

func wordmark(font string) []string {
	raw := strings.TrimRight(figure.NewFigure("3270CONNECT", font, true).String(), "\n")
	lines := strings.Split(raw, "\n")
	// Dedent: figlet pads the left edge, which throws off centring.
	indent := 1 << 30
	for _, l := range lines {
		t := strings.TrimLeft(l, " ")
		if t == "" {
			continue
		}
		if n := len(l) - len(t); n < indent {
			indent = n
		}
	}
	for i, l := range lines {
		if len(l) >= indent {
			lines[i] = strings.TrimRight(l[indent:], " ")
		}
	}
	return lines
}

func wordmarkWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		if lipgloss.Width(l) > w {
			w = lipgloss.Width(l)
		}
	}
	return w
}

// ------------------------------------------------------------- box drawing --

// panelTop draws "╭─ TITLE ────────────────── right ─╮" at width w.
func panelTop(title, right string, w int) string {
	left := sLine.Render("╭─")
	used := 2
	if title != "" {
		left += " " + sEyebrow.Render(title) + " "
		used += lipgloss.Width(title) + 2
	}
	rightPart := ""
	if right != "" {
		rightPart = " " + sText3.Render(right) + " " + sLine.Render("─")
		used += lipgloss.Width(right) + 3
	}
	fill := w - used - 1 // -1 for ╮
	if fill < 0 {
		fill = 0
	}
	return left + sLine.Render(rule(fill)) + rightPart + sLine.Render("╮")
}

func panelBottom(w int) string {
	return sLine.Render("╰" + rule(w-2) + "╯")
}

// panelRow wraps already-styled content to the panel's inner width.
func panelRow(content string, w int) string {
	return sLine.Render("│") + pad(content, w-2) + sLine.Render("│")
}

// panelSplit draws an inner divider row.
func panelSplit(w int) string {
	return sLine.Render("├" + rule(w-2) + "┤")
}

// ------------------------------------------------------------------ pieces --

type kv struct {
	k, v  string
	style lipgloss.Style
}

// kvGrid lays key/value pairs into two columns inside a panel of width w.
func kvGrid(items []kv, w int, keyW int) []string {
	inner := w - 2
	colW := inner / 2
	var rows []string
	for i := 0; i < len(items); i += 2 {
		cell := func(it kv) string {
			st := it.style
			if st.String() == "" && it.style.GetForeground() == lipgloss.NoColor(struct{}{}) {
				st = sText
			}
			return "  " + sText3.Render(pad(it.k, keyW)) + st.Render(it.v)
		}
		line := pad(cell(items[i]), colW)
		if i+1 < len(items) {
			line += cell(items[i+1])
		}
		rows = append(rows, panelRow(line, w))
	}
	return rows
}

// meter renders a phosphor usage bar.
func meter(pct float64, width int) string {
	filled := int(pct / 100 * float64(width))
	if filled < 1 && pct > 0 {
		filled = 1
	}
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	col := cAccent
	switch {
	case pct >= 80:
		col = cDanger
	case pct >= 50:
		col = cWarn
	}
	return fg(col).Render(strings.Repeat("█", filled)) + sLine.Render(strings.Repeat("░", width-filled))
}

// tile renders a KPI tile body of a fixed width, returned as its 4 lines.
func tile(label, value, note, colour string, w int) []string {
	return []string{
		sLine.Render("╭" + rule(w-2) + "╮"),
		sLine.Render("│") + pad("  "+sText3.Render(label), w-2) + sLine.Render("│"),
		sLine.Render("│") + pad("  "+fg(colour).Bold(true).Render(value), w-2) + sLine.Render("│"),
		sLine.Render("│") + pad("  "+sText3.Render(note), w-2) + sLine.Render("│"),
		sLine.Render("╰" + rule(w-2) + "╯"),
	}
}

func joinTiles(tiles [][]string, gap int, indent int) []string {
	out := make([]string, len(tiles[0]))
	for row := range out {
		line := strings.Repeat(" ", indent)
		for i, t := range tiles {
			if i > 0 {
				line += strings.Repeat(" ", gap)
			}
			line += t[row]
		}
		out[row] = line
	}
	return out
}

// --------------------------------------------------------------- variant A --
// "Hairline" — no boxes. Rules, mono eyebrows, dim keys, bright values.

func variantA() {
	fmt.Println()
	for _, l := range wordmark("small") {
		fmt.Println("  " + gradient(l, cAccent, cInfo))
	}
	fmt.Println()
	fmt.Println("  " + sEyebrow.Render("MAINFRAME AUTOMATION TOOLKIT") + sLine.Render("  ·  ") +
		sText3.Render("hammering 3270 screens since 2023"))
	fmt.Println()
	fmt.Println("  " + sText3.Render("version ") + sText.Render("2.0.0") +
		sLine.Render("   ·   ") + sText3.Render("3270.io") +
		sLine.Render("   ·   ") + sText3.Render("EyUp.io") +
		sLine.Render("   ·   ") + sText3.Render("linux/amd64") +
		sLine.Render("   ·   ") + sText3.Render("pid 41207"))
	fmt.Println()
	fmt.Println("  " + sLine.Render(rule(W-4)))
	fmt.Println("  " + sEyebrow.Render("WORKFLOW") + sLine.Render("  ") + sText3.Render("workflow.json"))
	fmt.Println("  " + sLine.Render(rule(W-4)))
	fmt.Println()

	items := []kv{
		{"host", "127.0.0.1:3270", sAccent},
		{"output", "output.html", sText},
		{"step delay", "0.10s – 0.30s", sText},
		{"wait for field", "on · 1s × 10", sText},
		{"ramp up", "50 / 1.50s", sText},
		{"end of task", "60s – 120s", sText},
		{"grace period", "30s", sText},
		{"auto shutdown", "10s", sText},
	}
	keyW := 16
	colW := (W - 4) / 2
	for i := 0; i < len(items); i += 2 {
		cell := func(it kv) string {
			return sText3.Render(pad(it.k, keyW)) + it.style.Render(it.v)
		}
		line := pad(cell(items[i]), colW)
		if i+1 < len(items) {
			line += cell(items[i+1])
		}
		fmt.Println("  " + line)
	}
	fmt.Println()
	fmt.Println("  " + sText3.Render("cli") + "  " + sText3.Render("·") + " " + sText2.Render("-config workflow.json -headless"))
	fmt.Println()
}

func summaryA() {
	fmt.Println()
	fmt.Println("  " + fg(cAccent).Bold(true).Render("✓") + "  " + sText.Render("All workflows wrapped up") +
		sLine.Render("  ·  ") + sText3.Render("time for a victory lap"))
	fmt.Println()
	fmt.Println("  " + sLine.Render(rule(W-4)))
	fmt.Println("  " + sEyebrow.Render("RUN SUMMARY") + sLine.Render("  ") + sText3.Render("performance report") +
		lpad(sText3.Render("70s elapsed"), W-4-lipgloss.Width("RUN SUMMARY  performance report")))
	fmt.Println("  " + sLine.Render(rule(W-4)))
	fmt.Println()

	row := func(k, v, note string, vs lipgloss.Style, ns lipgloss.Style) {
		fmt.Println("  " + sText3.Render(pad(k, 26)) + vs.Render(pad(v, 12)) + ns.Render(note))
	}
	row("workflows started", "1", "▲ launched", sText, sText3)
	row("workflows completed", "1", "● all green", sAccent, fg(cAccent))
	row("workflows failed", "0", "● none", sText, sText3)
	fmt.Println()
	fmt.Println("  " + sText3.Render(pad("average cpu", 26)) + sText.Render(pad("1.1%", 12)) + meter(1.1, 24) + "  " + sText3.Render("chill"))
	fmt.Println("  " + sText3.Render(pad("average memory", 26)) + sText.Render(pad("2.1%", 12)) + meter(2.1, 24) + "  " + sText3.Render("chill"))
	fmt.Println()
	row("average workflow time", "69.57s", "◇ pace setter", sText, sText3)
	row("run duration", "70s", "◇ completed", sText, sText3)
	fmt.Println()
	fmt.Println("  " + sText3.Render("summary saved to ") + sText2.Render("logs/summary_41207.txt"))
	fmt.Println()
}

// --------------------------------------------------------------- variant B --
// "Console" — glass panels with rounded corners, mirroring the WebUI cards.

func variantB() {
	fmt.Println()
	fmt.Println(panelTop("", "", W))
	fmt.Println(panelRow("", W))
	wm := wordmark("small")
	lead := strings.Repeat(" ", ((W-2)-wordmarkWidth(wm))/2)
	for _, l := range wm {
		fmt.Println(panelRow(lead+gradient(l, cAccent, cInfo), W))
	}
	fmt.Println(panelRow("", W))
	fmt.Println(panelRow("  "+sEyebrow.Render("MAINFRAME AUTOMATION TOOLKIT")+
		sLine.Render("  ·  ")+sText3.Render("hammering 3270 screens since 2023"), W))
	fmt.Println(panelRow("", W))
	fmt.Println(panelSplit(W))
	strip := "  " + chip(cInk, cAccent).Render(" v2.0.0 ") +
		sLine.Render("  │  ") + sText2.Render("3270.io") +
		sLine.Render("  │  ") + sText2.Render("EyUp.io") +
		sLine.Render("  │  ") + sText3.Render("linux/amd64") +
		sLine.Render("  │  ") + sText3.Render("pid 41207")
	fmt.Println(panelRow(strip, W))
	fmt.Println(panelBottom(W))
	fmt.Println()

	fmt.Println(panelTop("WORKFLOW", "workflow.json", W))
	items := []kv{
		{"HOST", "127.0.0.1:3270", sAccent},
		{"OUTPUT", "output.html", sText},
		{"STEP DELAY", "0.10s – 0.30s", sText},
		{"WAIT FIELD", "on · 1s × 10", sText},
		{"RAMP UP", "50 / 1.50s", sText},
		{"END OF TASK", "60s – 120s", sText},
		{"GRACE", "30s", sText},
		{"AUTO SHUTDOWN", "10s", sText},
	}
	for _, r := range kvGrid(items, W, 16) {
		fmt.Println(r)
	}
	fmt.Println(panelSplit(W))
	fmt.Println(panelRow("  "+sText3.Render(pad("CLI", 16))+sText2.Render("-config workflow.json -headless"), W))
	fmt.Println(panelBottom(W))
	fmt.Println()
}

func summaryB() {
	fmt.Println()
	fmt.Println("  " + chip(cInk, cAccent).Render(" DONE ") + "  " + sText.Render("All workflows wrapped up") +
		sLine.Render("  ·  ") + sText3.Render("time for a victory lap"))
	fmt.Println()
	fmt.Println(panelTop("RUN SUMMARY", "70s elapsed", W))
	fmt.Println(panelRow("", W))

	tiles := [][]string{
		tile("STARTED", "1", "workflows", cText, 22),
		tile("COMPLETED", "1", "100.0%", cAccent, 22),
		tile("FAILED", "0", "clean run", cText3, 22),
	}
	for _, l := range joinTiles(tiles, 2, 0) {
		fmt.Println(panelRow("  "+l, W))
	}
	fmt.Println(panelRow("", W))
	fmt.Println(panelSplit(W))

	metric := func(label, value, note string, pct float64, useMeter bool) {
		body := "  " + sText3.Render(pad(label, 18)) + sText.Render(pad(value, 10))
		if useMeter {
			body += meter(pct, 26) + "  " + sText3.Render(note)
		} else {
			body += sText3.Render(note)
		}
		fmt.Println(panelRow(body, W))
	}
	metric("AVG CPU", "1.1%", "chill", 1.1, true)
	metric("AVG MEMORY", "2.1%", "chill", 2.1, true)
	metric("AVG WORKFLOW", "69.57s", "pace setter", 0, false)
	metric("RUN DURATION", "70s", "completed", 0, false)
	fmt.Println(panelBottom(W))
	fmt.Println("  " + sText3.Render("summary saved to ") + sText2.Render("logs/summary_41207.txt"))
	fmt.Println()
}

// --------------------------------------------------------------- variant C --
// "CRT" — heavier terminal fiction: scanline rules, bracketed status, cursor.

func variantC() {
	fmt.Println()
	scan := sLine.Render(strings.Repeat("▔", W))
	fmt.Println(scan)
	fmt.Println()
	for i, l := range wordmark("small") {
		shade := mix(cAccent, cInfo, float64(i)/3)
		fmt.Println("  " + fg(shade).Render(l))
	}
	fmt.Println("  " + sLine.Render(strings.Repeat("▁", W-4)))
	fmt.Println()
	fmt.Println("  " + sAccent.Render("▌") + " " + sEyebrow.Render("3270CONNECT") + sText3.Render(" v2.0.0") +
		sLine.Render("   ") + sText3.Render("mainframe automation toolkit") + " " + sAccent.Render("█"))
	fmt.Println("  " + sLine.Render("  ") + sText3.Render("3270.io · EyUp.io · linux/amd64 · pid 41207"))
	fmt.Println()
	fmt.Println("  " + sText3.Render("┌ ") + sEyebrow.Render("SESSION") + sText3.Render(" ") + sLine.Render(rule(W-16)))
	rows := [][2]string{
		{"HOST", "127.0.0.1:3270"},
		{"CONFIG", "workflow.json"},
		{"OUTPUT", "output.html"},
		{"STEP DELAY", "0.10s – 0.30s"},
		{"WAIT FIELD", "on · 1s × 10"},
		{"RAMP UP", "50 batch / 1.50s"},
		{"END OF TASK", "60s – 120s"},
		{"GRACE / SHUTDOWN", "30s / 10s"},
		{"CLI", "-config workflow.json -headless"},
	}
	for _, r := range rows {
		fmt.Println("  " + sText3.Render("│ ") + sText3.Render(pad(r[0], 18)) + sText.Render(r[1]))
	}
	fmt.Println("  " + sText3.Render("└") + sLine.Render(rule(W-4)))
	fmt.Println()
}

func summaryC() {
	fmt.Println()
	fmt.Println("  " + sAccent.Render("▌") + " " + fg(cAccent).Bold(true).Render("RUN COMPLETE") +
		sText3.Render("   all workflows wrapped up — time for a victory lap"))
	fmt.Println()
	fmt.Println("  " + sText3.Render("┌ ") + sEyebrow.Render("RUN SUMMARY") + " " +
		sLine.Render(rule(W-20)) + " " + sText3.Render("70s"))
	stat := func(icon, iconCol, label, value, note string) {
		fmt.Println("  " + sText3.Render("│ ") + fg(iconCol).Render(icon) + " " +
			sText3.Render(pad(label, 22)) + fg(iconCol).Bold(true).Render(pad(value, 10)) + sText3.Render(note))
	}
	stat("▲", cInfo, "workflows started", "1", "launched")
	stat("●", cAccent, "workflows completed", "1", "100.0%")
	stat("■", cText3, "workflows failed", "0", "clean run")
	fmt.Println("  " + sText3.Render("│"))
	bar := func(label, value string, pct float64, note string) {
		fmt.Println("  " + sText3.Render("│ ") + sText3.Render(pad(label, 24)) + sText.Render(pad(value, 8)) +
			meter(pct, 22) + " " + sText3.Render(note))
	}
	bar("average cpu", "1.1%", 1.1, "chill")
	bar("average memory", "2.1%", 2.1, "chill")
	fmt.Println("  " + sText3.Render("│"))
	stat("◇", cViolet, "average workflow time", "69.57s", "pace setter")
	stat("◷", cWarn, "run duration", "70s", "completed")
	fmt.Println("  " + sText3.Render("└") + sLine.Render(rule(W-4)))
	fmt.Println("  " + sText3.Render("  summary → ") + sText2.Render("logs/summary_41207.txt") + " " + sAccent.Render("█"))
	fmt.Println()
}

func main() {
	lipgloss.SetColorProfile(termenv.TrueColor)

	blocks := []struct {
		id string
		fn func()
	}{
		{"A-header", variantA},
		{"A-summary", summaryA},
		{"B-header", variantB},
		{"B-summary", summaryB},
		{"C-header", variantC},
		{"C-summary", summaryC},
	}
	for _, b := range blocks {
		fmt.Printf("@@@%s\n", b.id)
		b.fn()
	}
	fmt.Println("@@@end")
}
