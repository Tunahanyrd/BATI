package gui

import "gioui.org/io/key"

type shortcutAction uint8

const (
	shortcutNone shortcutAction = iota
	shortcutOverview
	shortcutHistory
	shortcutHealth
	shortcutAbout
	shortcutRefresh
	shortcutClearHover
)

func routeShortcut(event key.Event, textInputActive bool) shortcutAction {
	if event.State != key.Press {
		return shortcutNone
	}
	if event.Name == key.NameEscape {
		return shortcutClearHover
	}
	if textInputActive || event.Modifiers != 0 {
		return shortcutNone
	}
	switch event.Name {
	case key.Name("1"):
		return shortcutOverview
	case key.Name("2"):
		return shortcutHistory
	case key.Name("3"):
		return shortcutHealth
	case key.Name("4"):
		return shortcutAbout
	case key.Name("R"):
		return shortcutRefresh
	default:
		return shortcutNone
	}
}
