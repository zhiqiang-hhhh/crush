package dialog

import (
	"cmp"
	"slices"
	"sort"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/sahilm/fuzzy"
)

// ProvidersID is the identifier for the provider selection dialog.
const ProvidersID = "providers"

// ActionSelectProvider is returned when a provider is selected in the
// providers dialog.
type ActionSelectProvider struct {
	Provider catwalk.Provider
}

// Providers represents a dialog for selecting a provider to connect.
type Providers struct {
	com *common.Common

	keyMap struct {
		UpDown key.Binding
		Select key.Binding
		Close  key.Binding
	}
	list  *ProvidersList
	input textinput.Model
	help  help.Model
}

var _ Dialog = (*Providers)(nil)

// NewProviders creates a new Providers dialog showing unconfigured providers.
func NewProviders(com *common.Common) (*Providers, error) {
	t := com.Styles
	m := &Providers{}
	m.com = com

	h := help.New()
	h.Styles = t.DialogHelpStyles()
	m.help = h

	m.list = NewProvidersList(t)
	m.list.Focus()
	m.list.SetSelected(0)

	m.input = textinput.New()
	m.input.SetVirtualCursor(false)
	m.input.Placeholder = "Search providers"
	m.input.SetStyles(com.Styles.TextInput)
	m.input.Focus()

	m.keyMap.Select = key.NewBinding(
		key.WithKeys("enter", "ctrl+y"),
		key.WithHelp("enter", "connect"),
	)
	m.keyMap.UpDown = key.NewBinding(
		key.WithKeys("up", "down"),
		key.WithHelp("↑/↓", "choose"),
	)
	m.keyMap.Close = CloseKey

	m.setProviderItems()

	return m, nil
}

// ID implements Dialog.
func (m *Providers) ID() string {
	return ProvidersID
}

// HandleMsg implements Dialog.
func (m *Providers) HandleMsg(msg tea.Msg) Action {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, m.keyMap.Close):
			return ActionClose{}
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "ctrl+p"))):
			m.list.Focus()
			if m.list.IsSelectedFirst() {
				m.list.SelectLast()
			} else {
				m.list.SelectPrev()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "ctrl+n"))):
			m.list.Focus()
			if m.list.IsSelectedLast() {
				m.list.SelectFirst()
			} else {
				m.list.SelectNext()
			}
			m.list.ScrollToSelected()
		case key.Matches(msg, m.keyMap.Select):
			selectedItem := m.list.SelectedItem()
			if selectedItem == nil {
				break
			}

			providerItem, ok := selectedItem.(*ProviderItem)
			if !ok {
				break
			}

			return ActionSelectProvider{
				Provider: providerItem.provider,
			}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			value := m.input.Value()
			m.list.Focus()
			m.list.SetFilter(value)
			m.list.SelectFirst()
			m.list.ScrollToTop()
			return ActionCmd{cmd}
		}
	}
	return nil
}

// Cursor returns the cursor for the dialog.
func (m *Providers) Cursor() *tea.Cursor {
	return InputCursor(m.com.Styles, m.input.Cursor())
}

// Draw implements [Dialog].
func (m *Providers) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	t := m.com.Styles
	width := max(0, min(defaultModelsDialogMaxWidth, area.Dx()-t.Dialog.View.GetHorizontalBorderSize()))
	height := max(0, min(defaultDialogHeight, area.Dy()-t.Dialog.View.GetVerticalBorderSize()))
	innerWidth := width - t.Dialog.View.GetHorizontalFrameSize()
	heightOffset := t.Dialog.Title.GetVerticalFrameSize() + titleContentHeight +
		t.Dialog.InputPrompt.GetVerticalFrameSize() + inputContentHeight +
		t.Dialog.HelpView.GetVerticalFrameSize() +
		t.Dialog.View.GetVerticalFrameSize()

	m.input.SetWidth(max(0, innerWidth-t.Dialog.InputPrompt.GetHorizontalFrameSize()-1))
	m.list.SetSize(innerWidth, height-heightOffset)
	m.help.SetWidth(innerWidth)

	rc := NewRenderContext(t, width)
	rc.Title = "Providers"

	inputView := t.Dialog.InputPrompt.Render(m.input.View())
	rc.AddPart(inputView)

	listView := t.Dialog.List.Height(m.list.Height()).Render(m.list.Render())
	rc.AddPart(listView)

	rc.Help = m.help.View(m)

	cur := m.Cursor()
	view := rc.Render()
	DrawCenterCursor(scr, area, view, cur)
	return cur
}

// ShortHelp returns the short help view.
func (m *Providers) ShortHelp() []key.Binding {
	return []key.Binding{
		m.keyMap.UpDown,
		m.keyMap.Select,
		m.keyMap.Close,
	}
}

// FullHelp returns the full help view.
func (m *Providers) FullHelp() [][]key.Binding {
	return [][]key.Binding{m.ShortHelp()}
}

// setProviderItems populates the list with all known providers.
// Already-configured providers are shown with a "connected" indicator and
// selecting them triggers reconfiguration.
func (m *Providers) setProviderItems() {
	cfg := m.com.Config()

	// Use KnownProviders from the store which includes the Copilot fallback.
	providers := m.com.Store().KnownProviders()

	// Sort alphabetically by display name, with Hyper always first.
	slices.SortStableFunc(providers, func(a, b catwalk.Provider) int {
		switch {
		case a.ID == "hyper":
			return -1
		case b.ID == "hyper":
			return 1
		default:
			nameA := cmp.Or(a.Name, string(a.ID))
			nameB := cmp.Or(b.Name, string(b.ID))
			return strings.Compare(strings.ToLower(nameA), strings.ToLower(nameB))
		}
	})

	var items []*ProviderItem
	for _, provider := range providers {
		providerID := string(provider.ID)
		_, configured := cfg.Providers.Get(providerID)
		name := cmp.Or(provider.Name, providerID)
		items = append(items, NewProviderItem(m.com.Styles, provider, name, configured))
	}

	m.list.SetProviderItems(items...)
}

// ProviderItem represents a provider in the provider selection list.
type ProviderItem struct {
	provider   catwalk.Provider
	name       string
	configured bool
	t          *styles.Styles
	cache      map[int]string
	focused    bool
	m          fuzzy.Match
}

var _ ListItem = &ProviderItem{}

// NewProviderItem creates a new ProviderItem.
func NewProviderItem(t *styles.Styles, provider catwalk.Provider, name string, configured bool) *ProviderItem {
	return &ProviderItem{
		provider:   provider,
		name:       name,
		configured: configured,
		t:          t,
		cache:      make(map[int]string),
	}
}

// Filter implements ListItem.
func (p *ProviderItem) Filter() string {
	return p.name
}

// ID implements ListItem.
func (p *ProviderItem) ID() string {
	return string(p.provider.ID)
}

// Render implements ListItem.
func (p *ProviderItem) Render(width int) string {
	styles := ListItemStyles{
		ItemBlurred:     p.t.Dialog.NormalItem,
		ItemFocused:     p.t.Dialog.SelectedItem,
		InfoTextBlurred: p.t.Base,
		InfoTextFocused: p.t.Base,
	}
	info := ""
	if p.configured {
		info = "connected"
	}
	return renderItem(styles, p.name, info, p.focused, width, p.cache, &p.m)
}

// SetFocused implements ListItem.
func (p *ProviderItem) SetFocused(focused bool) {
	if p.focused != focused {
		p.cache = nil
	}
	p.focused = focused
}

// SetMatch implements ListItem.
func (p *ProviderItem) SetMatch(fm fuzzy.Match) {
	p.cache = nil
	p.m = fm
}

// ProvidersList is a list for provider items.
type ProvidersList struct {
	*list.List
	items []*ProviderItem
	query string
	t     *styles.Styles
}

// NewProvidersList creates a new ProvidersList.
func NewProvidersList(sty *styles.Styles) *ProvidersList {
	f := &ProvidersList{
		List: list.NewList(),
		t:    sty,
	}
	f.RegisterRenderCallback(list.FocusedRenderCallback(f.List))
	return f
}

// Len returns the number of provider items.
func (f *ProvidersList) Len() int {
	return len(f.items)
}

// SetProviderItems sets the provider items and updates the list.
func (f *ProvidersList) SetProviderItems(items ...*ProviderItem) {
	f.items = items
	f.rebuildList()
}

func (f *ProvidersList) rebuildList() {
	var listItems []list.Item
	for _, item := range f.items {
		listItems = append(listItems, item)
	}
	f.SetItems(listItems...)
}

// SetFilter sets the filter query and updates the visible items.
func (f *ProvidersList) SetFilter(q string) {
	f.query = q
	f.SetItems(f.VisibleItems()...)
}

// SetSelected overrides the base method to skip non-provider items.
func (f *ProvidersList) SetSelected(index int) {
	if index < 0 || index >= f.Len() {
		f.List.SetSelected(index)
		return
	}

	f.List.SetSelected(index)
	for {
		selectedItem := f.SelectedItem()
		if _, ok := selectedItem.(*ProviderItem); ok {
			return
		}
		f.List.SetSelected(index + 1)
		index++
		if index >= f.Len() {
			return
		}
	}
}

// SelectNext selects the next provider item.
func (f *ProvidersList) SelectNext() (v bool) {
	v = f.List.SelectNext()
	for v {
		if _, ok := f.SelectedItem().(*ProviderItem); ok {
			return v
		}
		v = f.List.SelectNext()
	}
	return v
}

// SelectPrev selects the previous provider item.
func (f *ProvidersList) SelectPrev() (v bool) {
	v = f.List.SelectPrev()
	for v {
		if _, ok := f.SelectedItem().(*ProviderItem); ok {
			return v
		}
		v = f.List.SelectPrev()
	}
	return v
}

// SelectFirst selects the first provider item.
func (f *ProvidersList) SelectFirst() (v bool) {
	v = f.List.SelectFirst()
	for v {
		if _, ok := f.SelectedItem().(*ProviderItem); ok {
			return v
		}
		v = f.List.SelectNext()
	}
	return v
}

// SelectLast selects the last provider item.
func (f *ProvidersList) SelectLast() (v bool) {
	v = f.List.SelectLast()
	for v {
		if _, ok := f.SelectedItem().(*ProviderItem); ok {
			return v
		}
		v = f.List.SelectPrev()
	}
	return v
}

// IsSelectedFirst checks if the selected item is the first provider item.
func (f *ProvidersList) IsSelectedFirst() bool {
	originalIndex := f.Selected()
	f.SelectFirst()
	isFirst := f.Selected() == originalIndex
	f.List.SetSelected(originalIndex)
	return isFirst
}

// IsSelectedLast checks if the selected item is the last provider item.
func (f *ProvidersList) IsSelectedLast() bool {
	originalIndex := f.Selected()
	f.SelectLast()
	isLast := f.Selected() == originalIndex
	f.List.SetSelected(originalIndex)
	return isLast
}

// VisibleItems returns the visible items after filtering.
func (f *ProvidersList) VisibleItems() []list.Item {
	query := strings.ToLower(strings.ReplaceAll(f.query, " ", ""))

	if query == "" {
		var items []list.Item
		for _, item := range f.items {
			item.SetMatch(fuzzy.Match{})
			items = append(items, item)
		}
		return items
	}

	names := make([]string, len(f.items))
	for i, item := range f.items {
		names[i] = item.Filter()
	}

	matches := fuzzy.Find(query, names)

	// Sort by original index to preserve order.
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Index < matches[j].Index
	})

	var items []list.Item
	for _, match := range matches {
		item := f.items[match.Index]
		item.SetMatch(match)
		items = append(items, item)
	}

	return items
}

// Render renders the provider list.
func (f *ProvidersList) Render() string {
	f.SetItems(f.VisibleItems()...)
	return f.List.Render()
}
