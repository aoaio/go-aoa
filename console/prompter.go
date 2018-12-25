package console

import (
	"fmt"
	"strings"
	"github.com/peterh/liner"
)

var Stdin = newTerminalPrompter()

type UserPrompter interface {

	PromptInput(prompt string) (string, error)

	PromptPassword(prompt string) (string, error)

	PromptConfirm(prompt string) (bool, error)

	SetHistory(history []string)

	AppendHistory(command string)

	ClearHistory()

	SetWordCompleter(completer WordCompleter)
}

type WordCompleter func(line string, pos int) (string, []string, string)

type terminalPrompter struct {
	*liner.State
	warned     bool
	supported  bool
	normalMode liner.ModeApplier
	rawMode    liner.ModeApplier
}

func newTerminalPrompter() *terminalPrompter {
	p := new(terminalPrompter)

	normalMode, _ := liner.TerminalMode()

	p.State = liner.NewLiner()
	rawMode, err := liner.TerminalMode()
	if err != nil || !liner.TerminalSupported() {
		p.supported = false
	} else {
		p.supported = true
		p.normalMode = normalMode
		p.rawMode = rawMode

		normalMode.ApplyMode()
	}
	p.SetCtrlCAborts(true)
	p.SetTabCompletionStyle(liner.TabPrints)
	p.SetMultiLineMode(true)
	return p
}

func (p *terminalPrompter) PromptInput(prompt string) (string, error) {
	if p.supported {
		p.rawMode.ApplyMode()
		defer p.normalMode.ApplyMode()
	} else {

		fmt.Print(prompt)
		prompt = ""
		defer fmt.Println()
	}
	return p.State.Prompt(prompt)
}

func (p *terminalPrompter) PromptPassword(prompt string) (passwd string, err error) {
	if p.supported {
		p.rawMode.ApplyMode()
		defer p.normalMode.ApplyMode()
		return p.State.PasswordPrompt(prompt)
	}
	if !p.warned {
		fmt.Println("!! Unsupported terminal, password will be echoed.")
		p.warned = true
	}

	fmt.Print(prompt)
	passwd, err = p.State.Prompt("")
	fmt.Println()
	return passwd, err
}

func (p *terminalPrompter) PromptConfirm(prompt string) (bool, error) {
	input, err := p.Prompt(prompt + " [y/N] ")
	if len(input) > 0 && strings.ToUpper(input[:1]) == "Y" {
		return true, nil
	}
	return false, err
}

func (p *terminalPrompter) SetHistory(history []string) {
	p.State.ReadHistory(strings.NewReader(strings.Join(history, "\n")))
}

func (p *terminalPrompter) AppendHistory(command string) {
	p.State.AppendHistory(command)
}

func (p *terminalPrompter) ClearHistory() {
	p.State.ClearHistory()
}

func (p *terminalPrompter) SetWordCompleter(completer WordCompleter) {
	p.State.SetWordCompleter(liner.WordCompleter(completer))
}
