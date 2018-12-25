package console

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Aurorachain/go-Aurora/internal/jsre"
	"github.com/Aurorachain/go-Aurora/internal/web3ext"
	"github.com/Aurorachain/go-Aurora/rpc"
	"github.com/mattn/go-colorable"
	"github.com/peterh/liner"
	"github.com/robertkrimen/otto"
)

var (
	passwordRegexp = regexp.MustCompile(`personal.[nus]`)
	onlyWhitespace = regexp.MustCompile(`^\s*$`)
	exit           = regexp.MustCompile(`^\s*exit\s*;*\s*$`)
)

const HistoryFile = "history"

const DefaultPrompt = "> "

type Config struct {
	DataDir  string
	DocRoot  string
	Client   *rpc.Client
	Prompt   string
	Prompter UserPrompter
	Printer  io.Writer
	Preload  []string
}

type Console struct {
	client   *rpc.Client
	jsre     *jsre.JSRE
	prompt   string
	prompter UserPrompter
	histPath string
	history  []string
	printer  io.Writer
}

func New(config Config) (*Console, error) {

	if config.Prompter == nil {
		config.Prompter = Stdin
	}
	if config.Prompt == "" {
		config.Prompt = DefaultPrompt
	}
	if config.Printer == nil {
		config.Printer = colorable.NewColorableStdout()
	}

	console := &Console{
		client:   config.Client,
		jsre:     jsre.New(config.DocRoot, config.Printer),
		prompt:   config.Prompt,
		prompter: config.Prompter,
		printer:  config.Printer,
		histPath: filepath.Join(config.DataDir, HistoryFile),
	}
	if err := os.MkdirAll(config.DataDir, 0700); err != nil {
		return nil, err
	}
	if err := console.init(config.Preload); err != nil {
		return nil, err
	}
	return console, nil
}

func (c *Console) init(preload []string) error {

	bridge := newBridge(c.client, c.prompter, c.printer)
	c.jsre.Set("jeth", struct{}{})

	jethObj, _ := c.jsre.Get("jeth")
	jethObj.Object().Set("send", bridge.Send)
	jethObj.Object().Set("sendAsync", bridge.Send)

	consoleObj, _ := c.jsre.Get("console")
	consoleObj.Object().Set("log", c.consoleOutput)
	consoleObj.Object().Set("error", c.consoleOutput)

	if err := c.jsre.Compile("bignumber.js", jsre.BigNumber_JS); err != nil {
		return fmt.Errorf("bignumber.js: %v", err)
	}
	if err := c.jsre.Compile("web3.js", jsre.Web3_JS); err != nil {
		return fmt.Errorf("web3.js: %v", err)
	}
	if _, err := c.jsre.Run("var Web3 = require('web3');"); err != nil {
		return fmt.Errorf("web3 require: %v", err)
	}
	if _, err := c.jsre.Run("var web3 = new Web3(jeth);"); err != nil {
		return fmt.Errorf("web3 provider: %v", err)
	}

	apis, err := c.client.SupportedModules()
	if err != nil {
		return fmt.Errorf("api modules: %v", err)
	}
	flatten := "var aoa = web3.aoa; var personal = web3.personal; "
	for api := range apis {
		if api == "web3" {
			continue
		}
		if file, ok := web3ext.Modules[api]; ok {

			if err = c.jsre.Compile(fmt.Sprintf("%s.js", api), file); err != nil {
				return fmt.Errorf("%s.js: %v", api, err)
			}
			flatten += fmt.Sprintf("var %s = web3.%s; ", api, api)
		} else if obj, err := c.jsre.Run("web3." + api); err == nil && obj.IsObject() {

			flatten += fmt.Sprintf("var %s = web3.%s; ", api, api)
		}
	}
	if _, err = c.jsre.Run(flatten); err != nil {
		return fmt.Errorf("namespace flattening: %v", err)
	}

	if c.prompter != nil {

		personal, err := c.jsre.Get("personal")
		if err != nil {
			return err
		}

		if obj := personal.Object(); obj != nil {
			if _, err = c.jsre.Run(`jeth.openWallet = personal.openWallet;`); err != nil {
				return fmt.Errorf("personal.openWallet: %v", err)
			}
			if _, err = c.jsre.Run(`jeth.unlockAccount = personal.unlockAccount;`); err != nil {
				return fmt.Errorf("personal.unlockAccount: %v", err)
			}
			if _, err = c.jsre.Run(`jeth.newAccount = personal.newAccount;`); err != nil {
				return fmt.Errorf("personal.newAccount: %v", err)
			}
			if _, err = c.jsre.Run(`jeth.sign = personal.sign;`); err != nil {
				return fmt.Errorf("personal.sign: %v", err)
			}
			obj.Set("openWallet", bridge.OpenWallet)
			obj.Set("unlockAccount", bridge.UnlockAccount)
			obj.Set("newAccount", bridge.NewAccount)
			obj.Set("sign", bridge.Sign)
		}
	}

	admin, err := c.jsre.Get("admin")
	if err != nil {
		return err
	}
	if obj := admin.Object(); obj != nil {
		obj.Set("sleepBlocks", bridge.SleepBlocks)
		obj.Set("sleep", bridge.Sleep)
		obj.Set("clearHistory", c.clearHistory)
	}

	for _, path := range preload {
		if err := c.jsre.Exec(path); err != nil {
			failure := err.Error()
			if ottoErr, ok := err.(*otto.Error); ok {
				failure = ottoErr.String()
			}
			return fmt.Errorf("%s: %v", path, failure)
		}
	}

	if c.prompter != nil {
		if content, err := ioutil.ReadFile(c.histPath); err != nil {
			c.prompter.SetHistory(nil)
		} else {
			c.history = strings.Split(string(content), "\n")
			c.prompter.SetHistory(c.history)
		}
		c.prompter.SetWordCompleter(c.AutoCompleteInput)
	}
	return nil
}

func (c *Console) clearHistory() {
	c.history = nil
	c.prompter.ClearHistory()
	if err := os.Remove(c.histPath); err != nil {
		fmt.Fprintln(c.printer, "can't delete history file:", err)
	} else {
		fmt.Fprintln(c.printer, "history file deleted")
	}
}

func (c *Console) consoleOutput(call otto.FunctionCall) otto.Value {
	output := []string{}
	for _, argument := range call.ArgumentList {
		output = append(output, fmt.Sprintf("%v", argument))
	}
	fmt.Fprintln(c.printer, strings.Join(output, " "))
	return otto.Value{}
}

func (c *Console) AutoCompleteInput(line string, pos int) (string, []string, string) {

	if len(line) == 0 || pos == 0 {
		return "", nil, ""
	}

	start := pos - 1
	for ; start > 0; start-- {

		if line[start] == '.' || (line[start] >= 'a' && line[start] <= 'z') || (line[start] >= 'A' && line[start] <= 'Z') {
			continue
		}

		if start >= 3 && line[start-3:start] == "web3" {
			start -= 3
			continue
		}

		start++
		break
	}
	return line[:start], c.jsre.CompleteKeywords(line[start:pos]), line[pos:]
}

func (c *Console) Welcome() {

	fmt.Fprintf(c.printer, "Welcome to the Aurora JavaScript console!\n\n")
	c.jsre.Run(`
		console.log("instance: " + web3.version.node);
        console.log("at block: " + aoa.blockNumber + " (" + new Date(1000 * aoa.getBlock(aoa.blockNumber).timestamp) + ")");
		console.log(" datadir: " + admin.datadir);
	`)

	if apis, err := c.client.SupportedModules(); err == nil {
		modules := make([]string, 0, len(apis))
		for api, version := range apis {
			modules = append(modules, fmt.Sprintf("%s:%s", api, version))
		}
		sort.Strings(modules)
		fmt.Fprintln(c.printer, " modules:", strings.Join(modules, " "))
	}
	fmt.Fprintln(c.printer)
}

func (c *Console) Evaluate(statement string) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(c.printer, "[native] error: %v\n", r)
		}
	}()
	return c.jsre.Evaluate(statement, c.printer)
}

func (c *Console) Interactive() {
	var (
		prompt    = c.prompt
		indents   = 0
		input     = ""
		scheduler = make(chan string)
	)

	go func() {
		for {

			line, err := c.prompter.PromptInput(<-scheduler)
			if err != nil {

				if err == liner.ErrPromptAborted {
					prompt, indents, input = c.prompt, 0, ""
					scheduler <- ""
					continue
				}
				close(scheduler)
				return
			}

			scheduler <- line
		}
	}()

	abort := make(chan os.Signal, 1)
	signal.Notify(abort, os.Interrupt)

	for {

		scheduler <- prompt
		select {
		case <-abort:

			fmt.Fprintln(c.printer, "caught interrupt, exiting")
			return

		case line, ok := <-scheduler:

			if !ok || (indents <= 0 && exit.MatchString(line)) {
				return
			}
			if onlyWhitespace.MatchString(line) {
				continue
			}

			input += line + "\n"

			indents = countIndents(input)
			if indents <= 0 {
				prompt = c.prompt
			} else {
				prompt = strings.Repeat(".", indents*3) + " "
			}

			if indents <= 0 {
				if len(input) > 0 && input[0] != ' ' && !passwordRegexp.MatchString(input) {
					if command := strings.TrimSpace(input); len(c.history) == 0 || command != c.history[len(c.history)-1] {
						c.history = append(c.history, command)
						if c.prompter != nil {
							c.prompter.AppendHistory(command)
						}
					}
				}
				c.Evaluate(input)
				input = ""
			}
		}
	}
}

func countIndents(input string) int {
	var (
		indents     = 0
		inString    = false
		strOpenChar = ' '
		charEscaped = false
	)

	for _, c := range input {
		switch c {
		case '\\':

			if !charEscaped && inString {
				charEscaped = true
			}
		case '\'', '"':
			if inString && !charEscaped && strOpenChar == c {
				inString = false
			} else if !inString && !charEscaped {
				inString = true
				strOpenChar = c
			}
			charEscaped = false
		case '{', '(':
			if !inString {
				indents++
			}
			charEscaped = false
		case '}', ')':
			if !inString {
				indents--
			}
			charEscaped = false
		default:
			charEscaped = false
		}
	}

	return indents
}

func (c *Console) Execute(path string) error {
	return c.jsre.Exec(path)
}

func (c *Console) Stop(graceful bool) error {
	if err := ioutil.WriteFile(c.histPath, []byte(strings.Join(c.history, "\n")), 0600); err != nil {
		return err
	}
	if err := os.Chmod(c.histPath, 0600); err != nil {
		return err
	}
	c.jsre.Stop(graceful)
	return nil
}
