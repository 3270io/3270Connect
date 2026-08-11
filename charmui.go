package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
	"golang.org/x/term"
)

const (
	// TimestampFormat defines the HH:MM:SS format used in log timestamps
	// Matches the format used in update-binaries.ps1: [INFO] HH:mm:ss
	TimestampFormat = "15:04:05"
)

// Color and style helpers
type Color struct {
	value      string
	background bool
}

func (c Color) apply(style lipgloss.Style) lipgloss.Style {
	if c.value == "" {
		return style
	}
	if c.background {
		return style.Background(lipgloss.Color(c.value))
	}
	return style.Foreground(lipgloss.Color(c.value))
}

func (c Color) ToStyle() Style {
	return Style{style: c.apply(lipgloss.NewStyle())}
}

func (c Color) Sprint(text string) string {
	return c.ToStyle().Sprint(text)
}

func (c Color) Sprintf(format string, args ...interface{}) string {
	return c.ToStyle().Sprintf(format, args...)
}

type Attr struct {
	bold bool
}

type Style struct {
	style lipgloss.Style
}

func (s Style) Sprint(text string) string {
	return s.style.Render(text)
}

func (s Style) Sprintf(format string, args ...interface{}) string {
	return s.style.Render(fmt.Sprintf(format, args...))
}

func (s Style) Render(text string) string {
	return s.style.Render(text)
}

func (s Style) ToStyle() Style {
	return s
}

type Prefix struct {
	Text  string
	Style Style
}

// Message printer with a styled prefix.
type MessagePrinter struct {
	Prefix           Prefix
	style            lipgloss.Style
	IncludeTimestamp bool // When true, adds HH:MM:SS timestamp before the prefix
}

func (m MessagePrinter) Println(args ...interface{}) {
	m.print(fmt.Sprint(args...))
}

func (m MessagePrinter) Printf(format string, args ...interface{}) {
	m.print(fmt.Sprintf(format, args...))
}

func (m MessagePrinter) print(msg string) {
	var line string
	timestamp := ""
	if m.IncludeTimestamp {
		timestamp = " " + time.Now().Format(TimestampFormat)
	}
	if m.Prefix.Text != "" {
		// Add a small pad around the prefix for readability.
		prefix := m.Prefix.Style.Render(" " + m.Prefix.Text + " ")
		line = fmt.Sprintf("%s%s %s", prefix, timestamp, msg)
	} else {
		line = timestamp + msg
	}
	fmt.Println(m.style.Render(line))
}

// Section printer for headlines.
type SectionPrinter struct {
	Style Style
}

func (s SectionPrinter) WithStyle(style Style) *SectionPrinter {
	cp := s
	cp.Style = style
	return &cp
}

func (s SectionPrinter) Println(text string) {
	fmt.Println(s.Style.Render(text))
}

// Simple table support.
type TableData [][]string

type TablePrinter struct {
	data       TableData
	hasHeader  bool
	leftAlign  bool
	headerLine bool
}

func (t TablePrinter) WithHasHeader() *TablePrinter {
	cp := t
	cp.hasHeader = true
	return &cp
}

func (t TablePrinter) WithLeftAlignment() *TablePrinter {
	cp := t
	cp.leftAlign = true
	return &cp
}

func (t TablePrinter) WithData(data TableData) *TablePrinter {
	cp := t
	cp.data = data
	return &cp
}

func (t TablePrinter) Render() {
	if len(t.data) == 0 {
		return
	}

	colCount := len(t.data[0])
	widths := make([]int, colCount)
	for _, row := range t.data {
		for i, cell := range row {
			w := lipgloss.Width(cell)
			if w > widths[i] {
				widths[i] = w
			}
		}
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e5e7eb"))
	for rowIdx, row := range t.data {
		cells := make([]string, len(row))
		for i, cell := range row {
			pad := widths[i] - lipgloss.Width(cell)
			if pad < 0 {
				pad = 0
			}
			if t.leftAlign {
				cells[i] = cell + strings.Repeat(" ", pad)
			} else {
				cells[i] = strings.Repeat(" ", pad) + cell
			}
		}
		line := strings.Join(cells, "  ")
		if t.hasHeader && rowIdx == 0 {
			fmt.Println(headerStyle.Render(line))
			fmt.Println(strings.Repeat("─", lipgloss.Width(line)))
			continue
		}
		fmt.Println(line)
	}
}

// Progress bar support.
type progressbarBuilder struct {
	renderer *barRenderer
}

type ProgressbarPrinter struct {
	Title          string
	Total          int
	Current        int
	barChar        string
	barStyle       Style
	showPercentage bool
	showCount      bool
	showElapsed    bool
	writer         io.Writer
	start          time.Time
	renderer       *barRenderer
}

func (p progressbarBuilder) WithTotal(total int) *ProgressbarPrinter {
	renderer := p.renderer
	if renderer == nil {
		renderer = defaultBarRenderer
	}
	return &ProgressbarPrinter{
		Total:          total,
		barChar:        "█",
		barStyle:       Style{style: lipgloss.NewStyle().Foreground(lipgloss.Color("#22d3ee"))},
		showPercentage: true,
		showCount:      true,
		writer:         os.Stdout,
		start:          time.Now(),
		renderer:       renderer,
	}
}

func (p *ProgressbarPrinter) WithTitle(title string) *ProgressbarPrinter {
	p.Title = title
	return p
}

func (p *ProgressbarPrinter) WithWriter(w io.Writer) *ProgressbarPrinter {
	if w != nil {
		p.writer = w
	}
	return p
}

func (p *ProgressbarPrinter) WithBarCharacter(char string) *ProgressbarPrinter {
	if char != "" {
		p.barChar = char
	}
	return p
}

func (p *ProgressbarPrinter) WithBarStyle(style Style) *ProgressbarPrinter {
	p.barStyle = style
	return p
}

func (p *ProgressbarPrinter) WithShowPercentage(show bool) *ProgressbarPrinter {
	p.showPercentage = show
	return p
}

func (p *ProgressbarPrinter) WithShowCount(show bool) *ProgressbarPrinter {
	p.showCount = show
	return p
}

func (p *ProgressbarPrinter) WithShowElapsedTime(show bool) *ProgressbarPrinter {
	p.showElapsed = show
	return p
}

func (p *ProgressbarPrinter) WithTotal(total int) *ProgressbarPrinter {
	p.Total = total
	return p
}

func (p *ProgressbarPrinter) Start() (*ProgressbarPrinter, error) {
	p.start = time.Now()
	return p, nil
}

func (p *ProgressbarPrinter) UpdateTitle(title string) {
	p.Title = title
}

func (p *ProgressbarPrinter) view() string {
	total := p.Total
	if total <= 0 {
		total = 1
	}
	ratio := float64(p.Current) / float64(total)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	width := 28
	filled := int(ratio * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	empty := width - filled

	bar := p.barStyle.Render(strings.Repeat(p.barChar, filled) + strings.Repeat(" ", empty))

	parts := []string{p.Title, "[" + bar + "]"}
	if p.showPercentage {
		parts = append(parts, fmt.Sprintf("%3d%%", int(ratio*100)))
	}
	if p.showCount {
		parts = append(parts, fmt.Sprintf("%d/%d", p.Current, p.Total))
	}
	if p.showElapsed {
		elapsed := time.Since(p.start).Round(time.Second)
		parts = append(parts, elapsed.String())
	}
	return strings.Join(parts, "  ")
}

func (p *ProgressbarPrinter) render() {
	renderer := p.renderer
	if renderer == nil {
		renderer = defaultBarRenderer
	}
	renderer.Render(p)
}

type barRenderer struct {
	mu    sync.Mutex
	lines int
}

func (r *barRenderer) render(bars []*ProgressbarPrinter, extraRows []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(bars) == 0 && len(extraRows) == 0 {
		return
	}

	if r.lines > 0 {
		fmt.Printf("\033[%dA", r.lines)
	}

	lineCount := len(extraRows)
	for _, bar := range bars {
		if bar == nil {
			continue
		}
		fmt.Println(bar.view())
		lineCount++
	}
	for _, row := range extraRows {
		fmt.Println(row)
	}
	r.lines = lineCount
}

func (r *barRenderer) Render(bars ...*ProgressbarPrinter) {
	r.render(bars, nil)
}

func (r *barRenderer) RenderWithRows(bars []*ProgressbarPrinter, rows []string) {
	r.render(bars, rows)
}

func (r *barRenderer) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = 0
}

// MultiPrinter keeps multiple progress bars in sync.
type MultiPrinter struct {
	renderer *barRenderer
}

func (m *MultiPrinter) NewWriter() io.Writer {
	// Writer is ignored in this shim but kept for API compatibility.
	return io.Discard
}

func (m *MultiPrinter) Stop() {
	if m.renderer != nil {
		m.renderer.Reset()
	}
	fmt.Println()
}

// Spinner support.
type spinnerBuilder struct {
	removeWhenDone bool
}

type Spinner struct {
	message        string
	frames         []string
	idx            int
	done           chan struct{}
	removeWhenDone bool
	mu             sync.Mutex
}

func (s spinnerBuilder) WithRemoveWhenDone(remove bool) *spinnerBuilder {
	cp := s
	cp.removeWhenDone = remove
	return &cp
}

func (s spinnerBuilder) Start(message string) (*Spinner, error) {
	sp := &Spinner{
		message:        message,
		removeWhenDone: s.removeWhenDone,
		frames: []string{
			"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
		},
		done: make(chan struct{}),
	}
	sp.start()
	return sp, nil
}

func (s *Spinner) start() {
	s.tick()
	ticker := time.NewTicker(120 * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-s.done:
				return
			}
		}
	}()
}

func (s *Spinner) tick() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done == nil {
		return
	}
	frame := s.frames[s.idx%len(s.frames)]
	s.idx++
	fmt.Printf("\r%s %s", frame, s.message)
}

func (s *Spinner) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		// already closed
	default:
		close(s.done)
	}
	if s.removeWhenDone {
		clear := strings.Repeat(" ", lipgloss.Width(s.message)+4)
		fmt.Printf("\r%s\r", clear)
	} else {
		fmt.Println()
	}
}

func (s *Spinner) Success(msg string) {
	s.stop()
	pterm.Success.Println(msg)
}

func (s *Spinner) Fail(msg string, err error) {
	s.stop()
	if err != nil {
		pterm.Error.Println(fmt.Sprintf("%s %v", msg, err))
		return
	}
	pterm.Error.Println(msg)
}

func (s *Spinner) Warning(msg string, err error) {
	s.stop()
	if err != nil {
		pterm.Warning.Println(fmt.Sprintf("%s %v", msg, err))
		return
	}
	pterm.Warning.Println(msg)
}

// Primary facade to keep existing call sites intact.
type charmPterm struct {
	Info                *MessagePrinter
	Warning             *MessagePrinter
	Error               *MessagePrinter
	Success             *MessagePrinter
	DefaultSection      SectionPrinter
	DefaultTable        TablePrinter
	DefaultProgressbar  progressbarBuilder
	DefaultMultiPrinter MultiPrinter
	DefaultSpinner      spinnerBuilder

	Bold Attr

	FgCyan       Color
	FgWhite      Color
	FgLightGreen Color
	FgYellow     Color
	FgRed        Color
	FgMagenta    Color
	FgBlue       Color
	FgGreen      Color
	FgBlack      Color

	BgBlue   Color
	BgRed    Color
	BgGreen  Color
	BgYellow Color
}

func newCharmPterm() *charmPterm {
	renderer := &barRenderer{}
	ui := &charmPterm{
		Bold: Attr{bold: true},

		FgCyan:       Color{value: "#22d3ee"},
		FgWhite:      Color{value: "#e5e7eb"},
		FgLightGreen: Color{value: "#a3e635"},
		FgYellow:     Color{value: "#fbbf24"},
		FgRed:        Color{value: "#f87171"},
		FgMagenta:    Color{value: "#c084fc"},
		FgBlue:       Color{value: "#60a5fa"},
		FgGreen:      Color{value: "#34d399"},
		FgBlack:      Color{value: "#0f172a"},

		BgBlue:   Color{value: "#1d4ed8", background: true},
		BgRed:    Color{value: "#b91c1c", background: true},
		BgGreen:  Color{value: "#15803d", background: true},
		BgYellow: Color{value: "#ca8a04", background: true},
	}

	ui.Info = &MessagePrinter{
		Prefix:           Prefix{Text: "INFO", Style: ui.NewStyle(ui.BgBlue, ui.FgWhite)},
		style:            lipgloss.NewStyle(),
		IncludeTimestamp: true,
	}
	ui.Warning = &MessagePrinter{
		Prefix:           Prefix{Text: "WARN", Style: ui.NewStyle(ui.BgYellow, ui.FgBlack)},
		style:            lipgloss.NewStyle(),
		IncludeTimestamp: true,
	}
	ui.Error = &MessagePrinter{
		Prefix:           Prefix{Text: "ERROR", Style: ui.NewStyle(ui.BgRed, ui.FgWhite)},
		style:            lipgloss.NewStyle(),
		IncludeTimestamp: true,
	}
	ui.Success = &MessagePrinter{
		Prefix:           Prefix{Text: "SUCCESS", Style: ui.NewStyle(ui.BgGreen, ui.FgBlack)},
		style:            lipgloss.NewStyle(),
		IncludeTimestamp: true,
	}

	ui.DefaultSection = SectionPrinter{Style: ui.NewStyle(ui.FgCyan, ui.Bold)}
	ui.DefaultTable = TablePrinter{leftAlign: true}
	ui.DefaultProgressbar = progressbarBuilder{renderer: renderer}
	ui.DefaultMultiPrinter = MultiPrinter{renderer: renderer}
	ui.DefaultSpinner = spinnerBuilder{}

	return ui
}

func (p *charmPterm) NewStyle(parts ...interface{}) Style {
	style := lipgloss.NewStyle()
	for _, part := range parts {
		switch v := part.(type) {
		case Color:
			style = v.apply(style)
		case Attr:
			if v.bold {
				style = style.Bold(true)
			}
		case Style:
			style = style.Inherit(v.style)
		}
	}
	return Style{style: style}
}

func (p *charmPterm) LightGreen(text string) string {
	return p.FgLightGreen.ToStyle().Sprint(text)
}

func (p *charmPterm) LightYellow(text string) string {
	return Color{value: "#facc15"}.ToStyle().Sprint(text)
}

func (p *charmPterm) White(text string) string {
	return p.FgWhite.ToStyle().Sprint(text)
}

func (p *charmPterm) Sprintf(format string, args ...interface{}) string {
	return fmt.Sprintf(format, args...)
}

// ---------------------------------------------------------------------------
// Phosphor theme
//
// The header and the end-of-run summary share a palette with the web console
// and the docs, so the three surfaces read as one product. Tokens mirror
// templates/static/css/dashboard.css and docs/stylesheets/3270-theme.css.
// The live progress rows keep the older palette above and are unaffected.
// ---------------------------------------------------------------------------

const (
	themeAccent = "#4effb3" // --accent      phosphor green
	themeText   = "#e6fff5" // --text
	themeText2  = "#9fe6c8" // --text-2
	themeText3  = "#5f9e86" // --text-3
	themeLine   = "#1e5a44" // --line        alpha flattened onto --bg
	themeInfo   = "#5ad2ff" // --info
	themeWarn   = "#f7c36b" // --warn
	themeDanger = "#ff6f82" // --danger

	// themeWidth is the widest the header and summary will draw. 80 columns
	// is the width of the screens the tool drives; narrower terminals clamp.
	themeWidth    = 80
	themeMinWidth = 48
	themeIndent   = "  "
)

var (
	styleAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color(themeAccent))
	styleText    = lipgloss.NewStyle().Foreground(lipgloss.Color(themeText))
	styleText2   = lipgloss.NewStyle().Foreground(lipgloss.Color(themeText2))
	styleText3   = lipgloss.NewStyle().Foreground(lipgloss.Color(themeText3))
	styleLine    = lipgloss.NewStyle().Foreground(lipgloss.Color(themeLine))
	styleEyebrow = lipgloss.NewStyle().Foreground(lipgloss.Color(themeAccent)).Bold(true)
)

// themeContentWidth is the drawable width inside the standard indent.
func themeContentWidth() int {
	w := themeWidth
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		if tw, _, err := term.GetSize(fd); err == nil && tw > 0 && tw < w {
			w = tw
		}
	}
	if w < themeMinWidth {
		w = themeMinWidth
	}
	return w - 2*len(themeIndent)
}

func themePad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func themeRule(w int) string { return styleLine.Render(strings.Repeat("─", w)) }

func themeHexRGB(h string) (float64, float64, float64) {
	r, _ := strconv.ParseInt(h[1:3], 16, 32)
	g, _ := strconv.ParseInt(h[3:5], 16, 32)
	b, _ := strconv.ParseInt(h[5:7], 16, 32)
	return float64(r), float64(g), float64(b)
}

// themeMix interpolates between two #rrggbb colours.
func themeMix(from, to string, t float64) string {
	fr, fg, fb := themeHexRGB(from)
	tr, tg, tb := themeHexRGB(to)
	return fmt.Sprintf("#%02x%02x%02x",
		int(fr+(tr-fr)*t), int(fg+(tg-fg)*t), int(fb+(tb-fb)*t))
}

// themeGradient paints text left to right across two colours. span fixes the
// ramp to the whole wordmark so hue lines up column-wise across its rows.
func themeGradient(s, from, to string, span int) string {
	if span <= 1 {
		span = 2
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		colour := lipgloss.Color(themeMix(from, to, float64(i)/float64(span-1)))
		b.WriteString(lipgloss.NewStyle().Foreground(colour).Render(string(r)))
	}
	return b.String()
}

// themeWordmark renders the figlet wordmark and strips the font's left pad so
// it sits flush with everything else.
func themeWordmark(text string) []string {
	lines := strings.Split(strings.TrimRight(figure.NewFigure(text, "small", true).String(), "\n"), "\n")
	indent := -1
	for _, l := range lines {
		trimmed := strings.TrimLeft(l, " ")
		if trimmed == "" {
			continue
		}
		if n := len(l) - len(trimmed); indent < 0 || n < indent {
			indent = n
		}
	}
	if indent < 0 {
		indent = 0
	}
	for i, l := range lines {
		if len(l) >= indent {
			lines[i] = strings.TrimRight(l[indent:], " ")
		}
	}
	return lines
}

// RenderBanner draws the wordmark and the tagline that opens every run.
func (p *charmPterm) RenderBanner(title, subtitle string) {
	text := strings.TrimSpace(strings.ToUpper(strings.Join(filterEmpty([]string{title, subtitle}), " ")))
	if text == "" {
		text = "3270CONNECT"
	}

	lines := themeWordmark(text)
	span := 0
	for _, l := range lines {
		if lipgloss.Width(l) > span {
			span = lipgloss.Width(l)
		}
	}

	fmt.Println()
	for _, line := range lines {
		fmt.Println(themeIndent + themeGradient(line, themeAccent, themeInfo, span))
	}
	fmt.Println()
	fmt.Println(themeIndent + styleEyebrow.Render("MAINFRAME AUTOMATION TOOLKIT") +
		styleLine.Render("  ·  ") + styleText3.Render("hammering 3270 screens since 2023"))
}

// RenderIdentityStrip draws one dot-separated line of run identity, replacing
// the stack of INFO-prefixed lines the header used to print.
func (p *charmPterm) RenderIdentityStrip(lead string, leadValue string, rest ...string) {
	line := styleText3.Render(lead+" ") + styleText.Render(leadValue)
	for _, item := range rest {
		if strings.TrimSpace(item) == "" {
			continue
		}
		line += styleLine.Render("   ·   ") + styleText3.Render(item)
	}
	fmt.Println(themeIndent + line)
}

// RenderSectionRule draws a ruled section heading: a bold eyebrow, an optional
// note beside it, and an optional right-aligned value.
func (p *charmPterm) RenderSectionRule(label, note, right string) {
	w := themeContentWidth()
	heading := styleEyebrow.Render(label)
	if note != "" {
		heading += styleLine.Render("  ") + styleText3.Render(note)
	}
	if right != "" {
		gap := w - lipgloss.Width(heading) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
		heading += strings.Repeat(" ", gap) + styleText3.Render(right)
	}
	fmt.Println(themeIndent + themeRule(w))
	fmt.Println(themeIndent + heading)
	fmt.Println(themeIndent + themeRule(w))
}

// ThemeKV is one cell of the two-column configuration grid.
type ThemeKV struct {
	Key    string
	Value  string
	Accent bool // draw the value in the phosphor accent rather than plain text
}

// RenderKeyValueGrid lays pairs out in two columns, dim keys against bright
// values. Falls back to a single column when the terminal is too narrow.
func (p *charmPterm) RenderKeyValueGrid(items []ThemeKV, keyWidth int) {
	w := themeContentWidth()
	columns := 2
	colWidth := w / 2
	if colWidth < keyWidth+18 {
		columns = 1
		colWidth = w
	}

	cell := func(it ThemeKV) string {
		value := styleText.Render(it.Value)
		if it.Accent {
			value = styleAccent.Render(it.Value)
		}
		return styleText3.Render(themePad(it.Key, keyWidth)) + value
	}

	for i := 0; i < len(items); i += columns {
		line := ""
		for c := 0; c < columns && i+c < len(items); c++ {
			if c > 0 {
				line = themePad(line, colWidth*c)
			}
			line += cell(items[i+c])
		}
		fmt.Println(themeIndent + line)
	}
}

// RenderNote draws a dim key with a secondary value, used for the CLI line and
// the saved-summary path.
func (p *charmPterm) RenderNote(key, value string) {
	line := styleText3.Render(key)
	if value != "" {
		line += " " + styleText2.Render(value)
	}
	fmt.Println(themeIndent + line)
}

// themeStatLabelWidth / themeStatValueWidth keep the summary's three columns
// aligned whether the row carries a glyph, a meter or neither.
const (
	themeStatLabelWidth = 26
	themeStatValueWidth = 12
)

// ThemeTone selects the semantic colour of a summary row's marker and note.
type ThemeTone int

const (
	ToneNeutral ThemeTone = iota
	ToneGood
	ToneWarn
	ToneBad
	ToneInfo
)

func (t ThemeTone) style() lipgloss.Style {
	switch t {
	case ToneGood:
		return styleAccent
	case ToneWarn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeWarn))
	case ToneBad:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeDanger))
	case ToneInfo:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(themeInfo))
	default:
		return styleText3
	}
}

// RenderStatRow draws one summary metric: dim label, bright value, and a
// glyph-prefixed note in the row's semantic colour.
func (p *charmPterm) RenderStatRow(label, value, glyph, note string, tone ThemeTone) {
	toneStyle := tone.style()
	valueStyle := styleText
	if tone == ToneGood || tone == ToneBad {
		valueStyle = toneStyle
	}
	trailer := ""
	if note != "" {
		trailer = toneStyle.Render(strings.TrimSpace(glyph + " " + note))
	}
	fmt.Println(themeIndent +
		styleText3.Render(themePad(label, themeStatLabelWidth)) +
		themePad(valueStyle.Render(value), themeStatValueWidth) +
		trailer)
}

// RenderMeterRow draws a metric as a phosphor usage bar. The bar warms through
// amber to red as the reading climbs, matching the console's meters.
func (p *charmPterm) RenderMeterRow(label, value string, percent float64, note string) {
	width := themeContentWidth() - themeStatLabelWidth - themeStatValueWidth - lipgloss.Width(note) - 2
	if width < 8 {
		width = 8
	}
	if width > 24 {
		width = 24
	}

	filled := int(percent / 100 * float64(width))
	if filled < 1 && percent > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	colour := themeAccent
	switch {
	case percent >= 80:
		colour = themeDanger
	case percent >= 50:
		colour = themeWarn
	}
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(colour)).Render(strings.Repeat("█", filled)) +
		styleLine.Render(strings.Repeat("░", width-filled))

	fmt.Println(themeIndent +
		styleText3.Render(themePad(label, themeStatLabelWidth)) +
		themePad(styleText.Render(value), themeStatValueWidth) +
		bar + "  " + styleText3.Render(note))
}

// RenderOutcome draws the single line that opens the summary.
func (p *charmPterm) RenderOutcome(headline, note string, tone ThemeTone) {
	glyph := "✓"
	if tone == ToneBad {
		glyph = "✕"
	}
	line := tone.style().Bold(true).Render(glyph) + "  " + styleText.Render(headline)
	if note != "" {
		line += styleLine.Render("  ·  ") + styleText3.Render(note)
	}
	fmt.Println(themeIndent + line)
}

func filterEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (p *charmPterm) RenderProgressBars(bars ...*ProgressbarPrinter) {
	filtered := make([]*ProgressbarPrinter, 0, len(bars))
	for _, bar := range bars {
		if bar != nil {
			filtered = append(filtered, bar)
		}
	}
	if len(filtered) == 0 {
		return
	}
	renderer := defaultBarRenderer
	if p.DefaultMultiPrinter.renderer != nil {
		renderer = p.DefaultMultiPrinter.renderer
	}
	renderer.Render(filtered...)
}

func (p *charmPterm) RenderProgressBarsWithRows(bars []*ProgressbarPrinter, rows []string) {
	filtered := make([]*ProgressbarPrinter, 0, len(bars))
	for _, bar := range bars {
		if bar != nil {
			filtered = append(filtered, bar)
		}
	}
	if len(filtered) == 0 && len(rows) == 0 {
		return
	}
	renderer := defaultBarRenderer
	if p.DefaultMultiPrinter.renderer != nil {
		renderer = p.DefaultMultiPrinter.renderer
	}
	renderer.RenderWithRows(filtered, rows)
}

func (p *charmPterm) Println(args ...interface{}) {
	fmt.Println(args...)
}

var defaultBarRenderer = &barRenderer{}

// pterm is a drop-in shim backed by Charm (Lip Gloss) styling.
var pterm = newCharmPterm()
