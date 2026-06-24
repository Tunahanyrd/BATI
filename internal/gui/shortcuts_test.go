package gui

import (
	"testing"
	"time"

	"bati/internal/dto"
	"gioui.org/io/key"
)

func TestShortcutRouting(t *testing.T) {
	tests := []struct {
		name key.Name
		want shortcutAction
	}{
		{key.Name("1"), shortcutOverview},
		{key.Name("2"), shortcutHistory},
		{key.Name("3"), shortcutHealth},
		{key.Name("4"), shortcutAbout},
		{key.Name("R"), shortcutRefresh},
		{key.NameEscape, shortcutClearHover},
	}
	for _, test := range tests {
		got := routeShortcut(key.Event{Name: test.name, State: key.Press}, false)
		if got != test.want {
			t.Fatalf("key %q routed to %v, want %v", test.name, got, test.want)
		}
	}
}

func TestShortcutsRespectTextInput(t *testing.T) {
	if got := routeShortcut(key.Event{Name: key.Name("R"), State: key.Press}, true); got != shortcutNone {
		t.Fatalf("text input should suppress refresh shortcut, got %v", got)
	}
	if got := routeShortcut(key.Event{Name: key.NameEscape, State: key.Press}, true); got != shortcutClearHover {
		t.Fatalf("escape should remain available to dismiss hover, got %v", got)
	}
}

func TestRefreshResultPreservesSelectedPage(t *testing.T) {
	state := newUIState(nil)
	state.view = viewHealth
	state.loading = true
	state.results <- refreshResult{data: &dto.DashboardDTO{}, use24h: true, at: time.Now()}
	state.consumeRefreshResult()
	if state.view != viewHealth {
		t.Fatalf("refresh reset selected page to %v", state.view)
	}
}
