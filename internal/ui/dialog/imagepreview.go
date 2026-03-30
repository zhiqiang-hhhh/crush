package dialog

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	fimage "github.com/charmbracelet/crush/internal/ui/image"
	uv "github.com/charmbracelet/ultraviolet"
)

const ImagePreviewID = "image-preview"

type imagePreviewReadyMsg struct{}

type ImagePreview struct {
	com *common.Common

	imgEnc                      fimage.Encoding
	imgPrevWidth, imgPrevHeight int
	cellSizeW, cellSizeH        int
	isTmux                      bool

	att             message.Attachment
	decodedImg      image.Image
	previewingImage bool
	transmitting    bool

	km struct {
		Close key.Binding
	}
}

var _ Dialog = (*ImagePreview)(nil)

func NewImagePreview(com *common.Common, att message.Attachment, caps *common.Capabilities) (*ImagePreview, tea.Cmd) {
	d := &ImagePreview{
		com: com,
		att: att,
	}
	d.km.Close = key.NewBinding(
		key.WithKeys("esc", "alt+esc", "q", "enter"),
		key.WithHelp("esc/q", "close"),
	)
	if caps != nil {
		if caps.SupportsKittyGraphics() {
			d.imgEnc = fimage.EncodingKitty
		}
		d.cellSizeW, d.cellSizeH = caps.CellSize()
		_, d.isTmux = caps.Env.LookupEnv("TMUX")
	}

	// Eagerly decode so HandleMsg can transmit immediately once dimensions are known.
	d.decodedImg, _ = d.decodeImage()

	// Schedule a delayed tick so that HandleMsg runs after the first Draw sets dimensions.
	initCmd := tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return imagePreviewReadyMsg{}
	})
	return d, initCmd
}

func (d *ImagePreview) ID() string { return ImagePreviewID }

func (d *ImagePreview) CellSize() fimage.CellSize {
	return fimage.CellSize{
		Width:  d.cellSizeW,
		Height: d.cellSizeH,
	}
}

func (d *ImagePreview) imageID() string {
	return fmt.Sprintf("preview-%s", d.att.FileName)
}

func (d *ImagePreview) HandleMsg(msg tea.Msg) Action {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, d.km.Close) {
			return ActionClose{}
		}
	case tea.MouseClickMsg:
		return ActionClose{}
	case fimage.TransmittedMsg:
		d.previewingImage = true
		d.transmitting = false
		return ActionCmd{}
	case imagePreviewReadyMsg:
		// Keep ticking until the image is fully transmitted and visible.
	}

	if d.decodedImg != nil && d.imgPrevWidth > 0 && d.imgPrevHeight > 0 && !d.previewingImage && !d.transmitting {
		id := d.imageID()
		if fimage.HasTransmitted(id, d.imgPrevWidth, d.imgPrevHeight) {
			d.previewingImage = true
		} else {
			d.transmitting = true
			cmds = append(cmds, tea.Sequence(
				d.imgEnc.Transmit(id, d.decodedImg, d.CellSize(), d.imgPrevWidth, d.imgPrevHeight, d.isTmux),
				func() tea.Msg {
					return fimage.TransmittedMsg{ID: id}
				},
			))
		}
	}

	if !d.previewingImage {
		cmds = append(cmds, tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
			return imagePreviewReadyMsg{}
		}))
	}

	if len(cmds) > 0 {
		return ActionCmd{tea.Batch(cmds...)}
	}
	return nil
}

func (d *ImagePreview) decodeImage() (image.Image, error) {
	if len(d.att.Content) > 0 {
		img, _, err := image.Decode(bytes.NewReader(d.att.Content))
		return img, err
	}
	return loadImage(d.att.FilePath)
}

func (d *ImagePreview) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	maxWidth := min(80, area.Dx())
	maxHeight := min(30, area.Dy()-4)

	t := d.com.Styles
	innerWidth := maxWidth - t.Dialog.View.GetHorizontalFrameSize()
	imgPrevWidth := max(1, innerWidth-t.Dialog.ImagePreview.GetHorizontalFrameSize())
	imgPrevHeight := max(1, maxHeight-t.Dialog.ImagePreview.GetVerticalFrameSize())

	d.imgPrevWidth = imgPrevWidth
	d.imgPrevHeight = imgPrevHeight

	var preview string
	if d.previewingImage {
		preview = d.imgEnc.Render(d.imageID(), imgPrevWidth, imgPrevHeight)
	} else {
		var sb strings.Builder
		for y := range imgPrevHeight {
			for range imgPrevWidth {
				sb.WriteRune('█')
			}
			if y < imgPrevHeight-1 {
				sb.WriteRune('\n')
			}
		}
		preview = sb.String()
	}

	rc := NewRenderContext(t, maxWidth)
	rc.Gap = 1
	rc.Title = d.att.FileName

	imgView := t.Dialog.ImagePreview.Align(lipgloss.Center).Width(innerWidth).Render(preview)
	rc.AddPart(imgView)
	rc.Help = "esc/q: close"

	DrawCenter(scr, area, rc.Render())
	return nil
}
