package dialog

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/askuser"
	"github.com/charmbracelet/crush/internal/ui/common"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestAskUserCursorPosition(t *testing.T) {
	t.Parallel()

	com := common.DefaultCommon(nil)

	tests := []struct {
		name string
		req  askuser.QuestionRequest
	}{
		{
			name: "text_only",
			req: askuser.QuestionRequest{
				Question: "What is your name?",
			},
		},
		{
			name: "text_only_long_question",
			req: askuser.QuestionRequest{
				Question: "This is a very long question that should wrap to multiple lines in the dialog to test cursor positioning with multi-line questions",
			},
		},
		{
			name: "options_with_custom_text",
			req: askuser.QuestionRequest{
				Question:  "Pick one or type custom",
				Options:   []askuser.Option{{Label: "A"}, {Label: "B"}},
				AllowText: true,
			},
		},
		{
			name: "scrolling_question",
			req: askuser.QuestionRequest{
				Question: strings.Repeat("This is a very long question that needs scrolling. ", 20),
			},
		},
		{
			name: "body_with_text_input",
			req: askuser.QuestionRequest{
				Question:  "Approve this plan?",
				Body:      strings.Repeat("Step 1: do something important\n", 10),
				Options:   []askuser.Option{{Label: "Yes"}, {Label: "No"}},
				AllowText: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewAskUser(com, tt.req)

			// For options with custom text, switch to text mode.
			if len(tt.req.Options) > 0 && tt.req.AllowText {
				d.textMode = true
				d.input.Focus()
			}

			area := uv.Rectangle(image.Rect(0, 0, 80, 24))
			scr := uv.NewScreenBuffer(80, 24)
			cur := d.Draw(scr, area)

			require.NotNil(t, cur, "cursor should not be nil in text mode")

			// Read back screen content to find the input line.
			inputLineY := -1
			for y := range scr.Lines {
				lineStr := scr.Line(y).String()
				if strings.Contains(lineStr, "> ") {
					inputLineY = y
					break
				}
			}

			require.NotEqual(t, -1, inputLineY, "should find input line with prompt '> '")

			// Print screen for debugging.
			for y := range scr.Lines {
				lineStr := scr.Line(y).String()
				stripped := ansi.Strip(lineStr)
				if strings.TrimSpace(stripped) == "" {
					continue
				}
				marker := "  "
				if y == cur.Y {
					marker = "C>"
				}
				if y == inputLineY {
					marker = "I>"
				}
				if y == cur.Y && y == inputLineY {
					marker = "*>"
				}
				t.Logf("%s %2d: %q", marker, y, stripped)
			}

			t.Logf("cursor=(%d,%d), inputLine=%d", cur.X, cur.Y, inputLineY)

			require.Equal(t, inputLineY, cur.Y,
				"cursor Y should be on the input line (prompt '> ')")
		})
	}
}
