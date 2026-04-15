// Package logo renders a Smith wordmark in a stylized way.
package logo

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/MakeNowJust/heredoc"
	"github.com/zhiqiang-hhhh/smith/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/slice"
)

// letterform represents a letterform. It can be stretched horizontally by
// a given amount via the boolean argument.
type letterform func(bool) string

const diag = `╱`

// Opts are the options for rendering the Smith title art.
type Opts struct {
	FieldColor   color.Color // diagonal lines
	TitleColorA  color.Color // left gradient ramp point
	TitleColorB  color.Color // right gradient ramp point
	VersionColor color.Color // Version text color
	Width        int         // width of the rendered logo, used for truncation
}

// Render renders the Smith logo. Set the argument to true to render the narrow
// version, intended for use in a sidebar.
//
// The compact argument determines whether it renders compact for the sidebar
// or wider for the main pane.
func Render(s *styles.Styles, version string, compact bool, o Opts) string {
	fg := func(c color.Color, s string) string {
		return lipgloss.NewStyle().Foreground(c).Render(s)
	}

	// Title.
	const spacing = 1
	letterforms := []letterform{
		letterSStylized,
		letterM,
		letterI,
		letterT,
		letterH,
	}
	stretchIndex := -1 // -1 means no stretching.
	if !compact {
		stretchIndex = cachedRandN(len(letterforms))
	}

	smith := renderWord(spacing, stretchIndex, letterforms...)
	smithWidth := lipgloss.Width(smith)
	b := new(strings.Builder)
	for r := range strings.SplitSeq(smith, "\n") {
		fmt.Fprintln(b, styles.ApplyForegroundGrad(s, r, o.TitleColorA, o.TitleColorB))
	}
	smith = b.String()

	// Version row.
	version = ansi.Truncate(version, smithWidth, "…")
	gap := max(0, smithWidth-lipgloss.Width(version))
	metaRow := strings.Repeat(" ", gap) + fg(o.VersionColor, version)

	// Join the meta row and big Smith title.
	smith = strings.TrimSpace(metaRow + "\n" + smith)

	// Narrow version.
	if compact {
		field := fg(o.FieldColor, strings.Repeat(diag, smithWidth))
		return strings.Join([]string{field, field, smith, field, ""}, "\n")
	}

	fieldHeight := lipgloss.Height(smith)

	// Left field.
	const leftWidth = 6
	leftFieldRow := fg(o.FieldColor, strings.Repeat(diag, leftWidth))
	leftField := new(strings.Builder)
	for range fieldHeight {
		fmt.Fprintln(leftField, leftFieldRow)
	}

	// Right field.
	rightWidth := max(15, o.Width-smithWidth-leftWidth-2) // 2 for the gap.
	const stepDownAt = 0
	rightField := new(strings.Builder)
	for i := range fieldHeight {
		width := rightWidth
		if i >= stepDownAt {
			width = rightWidth - (i - stepDownAt)
		}
		fmt.Fprint(rightField, fg(o.FieldColor, strings.Repeat(diag, width)), "\n")
	}

	// Return the wide version.
	const hGap = " "
	logo := lipgloss.JoinHorizontal(lipgloss.Top, leftField.String(), hGap, smith, hGap, rightField.String())
	if o.Width > 0 {
		// Truncate the logo to the specified width.
		lines := strings.Split(logo, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, o.Width, "")
		}
		logo = strings.Join(lines, "\n")
	}
	return logo
}

// LandingRender renders a clean, centered Smith logo for the landing page.
// No flanking diagonal fields, no meta row — just the gradient block letters.
func LandingRender(t *styles.Styles, colorA, colorB color.Color) string {
	const spacing = 1
	letterforms := []letterform{
		letterSStylized,
		letterM,
		letterI,
		letterT,
		letterH,
	}
	smith := renderWord(spacing, -1, letterforms...)
	b := new(strings.Builder)
	for r := range strings.SplitSeq(smith, "\n") {
		fmt.Fprintln(b, styles.ApplyBoldForegroundGrad(t, r, colorA, colorB))
	}
	return strings.TrimRight(b.String(), "\n")
}

// SmallRender renders a smaller version of the Smith logo, suitable for
// smaller windows or sidebar usage.
func SmallRender(t *styles.Styles, width int) string {
	title := styles.ApplyBoldForegroundGrad(t, "Smith", t.Secondary, t.Primary)
	remainingWidth := width - lipgloss.Width(title) - 1 // 1 for the space after "Smith"
	if remainingWidth > 0 {
		lines := strings.Repeat("╱", remainingWidth)
		title = fmt.Sprintf("%s %s", title, t.Base.Foreground(t.Primary).Render(lines))
	}
	return title
}

// renderWord renders letterforms to fork a word. stretchIndex is the index of
// the letter to stretch, or -1 if no letter should be stretched.
func renderWord(spacing int, stretchIndex int, letterforms ...letterform) string {
	if spacing < 0 {
		spacing = 0
	}

	renderedLetterforms := make([]string, len(letterforms))

	// pick one letter randomly to stretch
	for i, letter := range letterforms {
		renderedLetterforms[i] = letter(i == stretchIndex)
	}

	if spacing > 0 {
		// Add spaces between the letters and render.
		renderedLetterforms = slice.Intersperse(renderedLetterforms, strings.Repeat(" ", spacing))
	}
	return strings.TrimSpace(
		lipgloss.JoinHorizontal(lipgloss.Top, renderedLetterforms...),
	)
}

// letterM renders the letter M in a stylized way.
func letterM(stretch bool) string {
	// Here's what we're making:
	//
	// █▄ ▄█
	// █ █ █
	// ▀   ▀

	left := heredoc.Doc(`
		█
		█
		▀
	`)
	topLeft := heredoc.Doc(`
		▄
		
	`)
	center := heredoc.Doc(`
		
		█
	`)
	topRight := heredoc.Doc(`
		▄
		
	`)
	right := heredoc.Doc(`
		█
		█
		▀
	`)
	_ = stretch
	return joinLetterform(left, topLeft, " ", center, " ", topRight, right)
}

// letterI renders the letter I in a stylized way.
func letterI(stretch bool) string {
	// Here's what we're making:
	//
	// ▀█▀
	//  █
	// ▀▀▀

	top := heredoc.Doc(`
		▀
		
		▀
	`)
	mid := heredoc.Doc(`
		█
		█
		▀
	`)
	_ = stretch
	return joinLetterform(top, mid, top)
}

// letterT renders the letter T in a stylized way.
func letterT(stretch bool) string {
	// Here's what we're making:
	//
	// ▀▀█▀▀
	//   █
	//   ▀

	side := heredoc.Doc(`
		▀

	`)
	center := heredoc.Doc(`
		█
		█
		▀
	`)
	return joinLetterform(
		stretchLetterformPart(side, letterformProps{
			stretch:    stretch,
			width:      2,
			minStretch: 4,
			maxStretch: 8,
		}),
		center,
		stretchLetterformPart(side, letterformProps{
			stretch:    stretch,
			width:      2,
			minStretch: 4,
			maxStretch: 8,
		}),
	)
}

// letterH renders the letter H in a stylized way. It takes an integer that
// determines how many cells to stretch the letter. If the stretch is less than
// 1, it defaults to no stretching.
func letterH(stretch bool) string {
	// Here's what we're making:
	//
	// █   █
	// █▀▀▀█
	// ▀   ▀

	side := heredoc.Doc(`
		█
		█
		▀`)
	middle := heredoc.Doc(`

		▀
	`)
	return joinLetterform(
		side,
		stretchLetterformPart(middle, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 8,
			maxStretch: 12,
		}),
		side,
	)
}

// letterSStylized renders the letter S in a stylized way.
func letterSStylized(stretch bool) string {
	// Here's what we're making:
	//
	// ▄▀▀▀▀▀
	// ▀▀▀▀▀█
	// ▀▀▀▀▀

	left := heredoc.Doc(`
		▄
		▀
		▀
	`)
	center := heredoc.Doc(`
		▀
		▀
		▀
	`)
	right := heredoc.Doc(`
		▀
		█
	`)
	return joinLetterform(
		left,
		stretchLetterformPart(center, letterformProps{
			stretch:    stretch,
			width:      3,
			minStretch: 7,
			maxStretch: 12,
		}),
		right,
	)
}

func joinLetterform(letters ...string) string {
	return lipgloss.JoinHorizontal(lipgloss.Top, letters...)
}

// letterformProps defines letterform stretching properties.
// for readability.
type letterformProps struct {
	width      int
	minStretch int
	maxStretch int
	stretch    bool
}

// stretchLetterformPart is a helper function for letter stretching. If randomize
// is false the minimum number will be used.
func stretchLetterformPart(s string, p letterformProps) string {
	if p.maxStretch < p.minStretch {
		p.minStretch, p.maxStretch = p.maxStretch, p.minStretch
	}
	n := p.width
	if p.stretch {
		n = cachedRandN(p.maxStretch-p.minStretch) + p.minStretch //nolint:gosec
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = s
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
