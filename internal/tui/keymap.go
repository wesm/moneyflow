package tui

import "charm.land/bubbles/v2/key"

type keyMap struct {
	up, down, top, group, detail, accounts key.Binding
	drill, back, time, clearTime           key.Binding
	left, right, sort, reverse             key.Binding
	selectOne, selectAll, quit             key.Binding
}

func defaultKeyMap() keyMap {
	return keyMap{
		up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		top:       key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first row")),
		group:     key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "group by")),
		detail:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "detail")),
		accounts:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "accounts")),
		drill:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "drill down")),
		back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		time:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "time grain")),
		clearTime: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "clear time")),
		left:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "previous")),
		right:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next")),
		sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		reverse:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "reverse")),
		selectOne: key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "select")),
		selectAll: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "select all")),
		quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}
