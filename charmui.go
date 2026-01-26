package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	figure "github.com/common-nighthawk/go-figure"
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
	Prefix        Prefix
	style         lipgloss.Style
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
		timestamp = time.Now().Format("15:04:05") + " "
	}
	if m.Prefix.Text != "" {
		// Add a small pad around the prefix for readability.
		prefix := m.Prefix.Style.Render(" " + m.Prefix.Text + " ")
		line = fmt.Sprintf("%s%s %s", timestamp, prefix, msg)
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
		Prefix: Prefix{Text: "INFO", Style: ui.NewStyle(ui.BgBlue, ui.FgWhite)},
		style:  lipgloss.NewStyle(),
	}
	ui.Warning = &MessagePrinter{
		Prefix: Prefix{Text: "WARN", Style: ui.NewStyle(ui.BgYellow, ui.FgBlack)},
		style:  lipgloss.NewStyle(),
	}
	ui.Error = &MessagePrinter{
		Prefix:           Prefix{Text: "ERROR", Style: ui.NewStyle(ui.BgRed, ui.FgWhite)},
		style:            lipgloss.NewStyle(),
		IncludeTimestamp: true, // Add timestamp to ERROR logs
	}
	ui.Success = &MessagePrinter{
		Prefix: Prefix{Text: "SUCCESS", Style: ui.NewStyle(ui.BgGreen, ui.FgBlack)},
		style:  lipgloss.NewStyle(),
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

func (p *charmPterm) RenderBanner(title, subtitle string) {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#0c6600")).Bold(true)
	shadow := lipgloss.NewStyle().Foreground(lipgloss.Color("#00bb2fff"))
	highlight := lipgloss.NewStyle().Foreground(lipgloss.Color("#00e927ff")).Bold(true)

	text := strings.TrimSpace(strings.ToUpper(strings.TrimSpace(strings.Join(filterEmpty([]string{title, subtitle}), " "))))
	if text == "" {
		text = "3270CONNECT"
	}

	fig := figure.NewFigure(text, "", true)
	raw := strings.TrimRight(fig.String(), "\n")
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		if i%2 == 0 {
			fmt.Println(accent.Render(line))
		} else {
			fmt.Println(shadow.Render(line))
		}
	}

	fmt.Println()
	tagline := "🔨 Hammering 3270 screens since 2023"
	fmt.Println(highlight.Render(strings.ToUpper(tagline)))
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
