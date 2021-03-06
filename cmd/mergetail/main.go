package main

import (
	"bufio"
	"net/http"
	"os"
	"os/exec"

	"github.com/account-login/mergetail"

	log "github.com/cihub/seelog"
	"github.com/mattn/go-shellwords"

	"strings"

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

type progArgs struct {
	placeHolder string
	hasTemplate bool
	template    []string
}

func getProgArgs() (args progArgs, err error) {
	args.placeHolder = "{}"

	i := 0
	for i = 0; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "-I") {
			// placeholder in template
			if os.Args[i] == "-I" {
				i++
				if i < len(os.Args) {
					args.placeHolder = os.Args[i]
				} else {
					err = errors.New("expect placeholder after -I")
					return
				}
			} else {
				args.placeHolder = os.Args[i][2:]
			}
		} else if os.Args[i] == "-t" {
			// template
			args.hasTemplate = true
			for _, piece := range os.Args[i+1:] {
				var parsed []string
				parsed, err = shellwords.Parse(piece)
				if err != nil {
					err = errors.Wrapf(err, "bad arg: %v", piece)
					return
				}
				args.template = append(args.template, parsed...)
			}
			if len(args.template) == 0 {
				err = errors.New("empty template")
				return
			}
			break
		}
	}

	return
}

func fillTemplate(template []string, placeHolder string, target string) []string {
	var ret []string
	for _, t := range template {
		ret = append(ret, strings.Replace(t, placeHolder, target, -1))
	}
	return ret
}

func StartDebugServer(addr string) {
	go func() {
		err := http.ListenAndServe(addr, nil)
		if err != nil {
			log.Errorf("failed to start debug server on %v: %v", addr, err)
		} else {
			log.Debugf("debug server started on %v", addr)
		}
	}()
}

func ConfigLogging() {
	logger, err := log.LoggerFromConfigAsString(`
<seelog>
	<outputs formatid="common">
		<console />
	</outputs>
    <formats>
        <format id="common" format="%Date(2006-01-02 15:04:05.000) [%LEVEL]%t%Msg%n"/>
    </formats>
</seelog>`)
	if err != nil {
		log.Errorf("log.LoggerFromConfigAsString() failed: %v", err)
		return
	}

	err = log.ReplaceLogger(logger)
	if err != nil {
		log.Errorf("log.ReplaceLogger() failed: %v", err)
	}
}

func realMain() int {
	defer log.Flush()

	ConfigLogging()
	StartDebugServer(":8899")

	args, err := getProgArgs()
	if err != nil {
		log.Error(err)
		return 1
	}

	tcmds := make([]mergetail.TailCmd, 0)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 { // skip empty line
			continue
		}

		if args.hasTemplate {
			// generate cmds from template supplied from -t ...
			cmdArgs := fillTemplate(args.template, args.placeHolder, line)
			tcmds = append(tcmds, mergetail.TailCmd{
				Cmd:    exec.Command(cmdArgs[0], cmdArgs[1:]...),
				Prefix: line,
			})
		} else {
			// read cmds directly
			tc, err := parseCmdLine(line)
			if err != nil {
				log.Error(err)
				return 2
			}
			tcmds = append(tcmds, tc)
		}
	}

	err = scanner.Err()
	if err != nil {
		log.Error(err)
		return 3
	}

	// main logic
	err = mergetail.MergeTail(tcmds, os.Stdout)
	if err != nil {
		log.Error(err)
		return 4
	}

	return 0
}

func main() {
	os.Exit(realMain())
}
