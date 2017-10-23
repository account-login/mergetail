package main

import (
	"bufio"
	"mergetail"
	"os"
	"os/exec"

	log "github.com/cihub/seelog"
	"github.com/mattn/go-shellwords"

	"github.com/pkg/errors"
)

func parseCmdLine(line string) (tc mergetail.TailCmd, err error) {
	var args []string
	args, err = shellwords.Parse(line)
	if err != nil {
		return
	}

	if len(args) < 2 {
		err = errors.Errorf("too few args: %v", args)
		return
	}

	prefix := args[0]
	prog := args[1]
	rest := args[2:]
	tc = mergetail.TailCmd{Cmd: exec.Command(prog, rest...), Prefix: prefix}
	return
}

func realMain() int {
	defer log.Flush()

	tcmds := make([]mergetail.TailCmd, 0)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		tc, err := parseCmdLine(line)
		if err != nil {
			log.Error(err)
			return 1
		}

		tcmds = append(tcmds, tc)
	}

	err := scanner.Err()
	if err != nil {
		log.Error(err)
		return 2
	}

	err = mergetail.MergeTail(tcmds, os.Stdout)
	if err != nil {
		log.Error(err)
		return 3
	}

	return 0
}

func main() {
	os.Exit(realMain())
}
