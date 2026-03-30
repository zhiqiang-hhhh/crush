package list

import (
	"fmt"
	"testing"
)

type testScrollItem struct {
	content string
}

func (t testScrollItem) Render(width int) string {
	return t.content
}

func TestScrollInfo(t *testing.T) {
	l := NewList()
	l.SetSize(80, 10)
	l.SetGap(1)

	// 20 items with gap=1 => 20 lines + 19 gaps = 39 total
	for i := 0; i < 20; i++ {
		l.AppendItems(testScrollItem{content: fmt.Sprintf("line %d", i)})
	}

	l.ScrollToTop()
	total, offset := l.ScrollInfo()
	fmt.Printf("At top: totalHeight=%d, offset=%d, height=%d, atBottom=%v\n", total, offset, l.Height(), l.AtBottom())

	l.ScrollToBottom()
	total, offset = l.ScrollInfo()
	fmt.Printf("At bottom: totalHeight=%d, offset=%d, height=%d, atBottom=%v\n", total, offset, l.Height(), l.AtBottom())

	l.ScrollBy(-5)
	total, offset = l.ScrollInfo()
	fmt.Printf("Up 5: totalHeight=%d, offset=%d, height=%d, atBottom=%v\n", total, offset, l.Height(), l.AtBottom())

	// Verify scrollbar would render
	if total <= l.Height() {
		t.Error("scrollbar should render: totalHeight should exceed viewport")
	}
	if total > l.Height() {
		fmt.Printf("Scrollbar WILL render (total %d > viewport %d)\n", total, l.Height())
	}
}
