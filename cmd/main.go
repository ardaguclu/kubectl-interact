package main

import (
	"os"

	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/ardaguclu/kubectl-interact/pkg/cmd"
)

func main() {
	flags := pflag.NewFlagSet("kubectl-interact", pflag.ExitOnError)
	pflag.CommandLine = flags

	root := cmd.NewCmdInteract(genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
