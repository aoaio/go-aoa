package main

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/Aurorachain/go-Aurora/cmd/utils"
	"github.com/Aurorachain/go-Aurora/node"
	"github.com/Aurorachain/go-Aurora/params"
	"github.com/naoina/toml"
	"gopkg.in/urfave/cli.v1"
	"io"
	"os"
	"reflect"
	"unicode"
	"github.com/Aurorachain/go-Aurora/aoa"
)

var (
	dumpConfigCommand = cli.Command{
		Action:      utils.MigrateFlags(dumpConfig),
		Name:        "dumpconfig",
		Usage:       "Show configuration values",
		ArgsUsage:   "",
		Flags:       append(append(nodeFlags, rpcFlags...), whisperFlags...),
		Category:    "MISCELLANEOUS COMMANDS",
		Description: `The dumpconfig command shows configuration values.`,
	}

	configFileFlag = cli.StringFlag{
		Name:  "config",
		Usage: "TOML configuration file",
	}
)

var tomlSettings = toml.Config{
	NormFieldName: func(rt reflect.Type, key string) string {
		return key
	},
	FieldToKey: func(rt reflect.Type, field string) string {
		return field
	},
	MissingField: func(rt reflect.Type, field string) error {
		link := ""
		if unicode.IsUpper(rune(rt.Name()[0])) && rt.PkgPath() != "main" {
			link = fmt.Sprintf(", see https://godoc.org/%s#%s for available fields", rt.PkgPath(), rt.Name())
		}
		return fmt.Errorf("field '%s' is not defined in %s%s", field, rt.String(), link)
	},
}

type aoastatsConfig struct {
	URL string `toml:",omitempty"`
}

type gaoaConfig struct {
	Aoa aoa.Config

	Node     node.Config
	Aoastats aoastatsConfig

}

func loadConfig(file string, cfg *gaoaConfig) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	err = tomlSettings.NewDecoder(bufio.NewReader(f)).Decode(cfg)

	if _, ok := err.(*toml.LineError); ok {
		err = errors.New(file + ", " + err.Error())
	}
	return err
}

func defaultNodeConfig() node.Config {
	cfg := node.DefaultConfig
	cfg.Name = clientIdentifier
	cfg.Version = params.VersionWithCommit(gitCommit)
	cfg.HTTPModules = append(cfg.HTTPModules, "aoa", "shh")
	cfg.WSModules = append(cfg.WSModules, "aoa", "shh")
	cfg.IPCPath = "aoa.ipc"
	return cfg
}

func makeConfigNode(ctx *cli.Context) (*node.Node, gaoaConfig) {

	cfg := gaoaConfig{
		Aoa: aoa.DefaultConfig,

		Node: defaultNodeConfig(),

	}

	if file := ctx.GlobalString(configFileFlag.Name); file != "" {
		if err := loadConfig(file, &cfg); err != nil {
			utils.Fatalf("%v", err)
		}
	}

	utils.SetNodeConfig(ctx, &cfg.Node)
	stack, err := node.New(&cfg.Node)
	if err != nil {
		utils.Fatalf("Failed to create the protocol stack: %v", err)
	}
	utils.SetAoaConfig(ctx, stack, &cfg.Aoa)
	if ctx.GlobalIsSet(utils.EthStatsURLFlag.Name) {
		cfg.Aoastats.URL = ctx.GlobalString(utils.EthStatsURLFlag.Name)
	}

	return stack, cfg
}

func enableWhisper(ctx *cli.Context) bool {
	for _, flag := range whisperFlags {
		if ctx.GlobalIsSet(flag.GetName()) {
			return true
		}
	}
	return false
}

func makeFullNode(ctx *cli.Context) *node.Node {
	stack, cfg := makeConfigNode(ctx)

	utils.RegisterAoaService(stack, &cfg.Aoa)

	if cfg.Aoastats.URL != "" {
		utils.RegisterAoaStatsService(stack, cfg.Aoastats.URL)
	}

	return stack
}

func dumpConfig(ctx *cli.Context) error {
	_, cfg := makeConfigNode(ctx)
	comment := ""

	if cfg.Aoa.Genesis != nil {
		cfg.Aoa.Genesis = nil
		comment += "# Note: this config doesn't contain the genesis block.\n\n"
	}

	out, err := tomlSettings.Marshal(&cfg)
	if err != nil {
		return err
	}
	io.WriteString(os.Stdout, comment)
	os.Stdout.Write(out)
	return nil
}
