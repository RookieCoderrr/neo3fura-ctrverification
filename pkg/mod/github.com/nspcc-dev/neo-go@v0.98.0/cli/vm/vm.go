package vm

import (
	"os"

	"github.com/abiosoft/readline"
	vmcli "github.com/nspcc-dev/neo-go/pkg/vm/cli"
	"github.com/urfave/cli"
)

// NewCommands returns 'vm' command.
func NewCommands() []cli.Command {
	return []cli.Command{{
		Name:   "vm",
		Usage:  "start the virtual machine",
		Action: startVMPrompt,
		Flags: []cli.Flag{
			cli.BoolFlag{Name: "debug, d"},
		},
	}}
}

func startVMPrompt(ctx *cli.Context) error {
	p := vmcli.NewWithConfig(true, os.Exit, &readline.Config{
		Stdout: ctx.App.Writer,
		Stderr: ctx.App.ErrWriter,
	})
	return p.Run()
}
