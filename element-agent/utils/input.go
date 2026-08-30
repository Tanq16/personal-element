package utils

import (
	"errors"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

var ErrNoTerminal = errors.New("no interactive terminal")

type inputModel struct {
	textInput textinput.Model
	done      bool
	value     string
	initCmd   tea.Cmd
}

func (m inputModel) Init() tea.Cmd { return m.initCmd }

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "enter":
			m.value = m.textInput.Value()
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.done = true
			return m, tea.Quit
		}
	}
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m inputModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(m.textInput.View())
}

func PromptInput(prompt, placeholder string) (string, error) {
	if !StdinIsTerminal {
		return "", ErrNoTerminal
	}
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = prompt + " "
	m := inputModel{textInput: ti, initCmd: ti.Focus()}

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(final.(inputModel).value), nil
}

func PromptPassword(prompt string) (string, error) {
	if !StdinIsTerminal {
		return "", ErrNoTerminal
	}
	ti := textinput.New()
	ti.Placeholder = "••••••••"
	ti.Prompt = prompt + " "
	ti.EchoMode = textinput.EchoPassword
	m := inputModel{textInput: ti, initCmd: ti.Focus()}

	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	return final.(inputModel).value, nil
}
