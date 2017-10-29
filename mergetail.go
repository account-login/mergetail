package mergetail

import (
	"bufio"
	"io"
	"os/exec"
	"sync"

	"fmt"

	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
)

// TODO: terminate child processes

type TailCmd struct {
	Cmd    *exec.Cmd
	Prefix string
}

type cmdContext struct {
	stdout io.ReadCloser
	stderr io.ReadCloser
	prefix string
	index  int
}

func colorize(str string, num int) string {
	code := (num % 230) + 1
	if code >= 16 {
		code++
	}
	fg := "\x1b[30m"
	m := (code - 4) / 6
	if m == 2 || m == 8 || m == 14 || code == 4 || code == 8 {
		fg = ""
	}
	return fmt.Sprintf("\x1b[48;5;%dm%s%s\x1b[0m", code, fg, str)
}

func makeStdoutLine(ctx *cmdContext, line string) string {
	return ctx.prefix + " " + line
}

func makeStderrLine(ctx *cmdContext, line string) string {
	return ctx.prefix + " \x1b[1m" + line + "\x1b[0m"
}

type lineMakerFunc func(string) string

func handleOutput(reader io.ReadCloser, lineMaker lineMakerFunc,
	stdxxxch chan<- string, errch chan<- error, wg *sync.WaitGroup) {

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		stdxxxch <- lineMaker(scanner.Text())
	}

	err := scanner.Err()
	if err != nil {
		errch <- err
	}
	err = reader.Close()
	if err != nil {
		errch <- err
	}
	wg.Done()
}

func mergeCmds(ctxlist []*cmdContext) (stdoutch chan string, stderrch chan string, errch chan error) {
	stdoutch = make(chan string)
	stderrch = make(chan string)
	errch = make(chan error)

	var wg sync.WaitGroup
	wg.Add(len(ctxlist) * 2)
	for _, ctx := range ctxlist {
		ctx := ctx
		go handleOutput(
			ctx.stdout, func(line string) string { return makeStdoutLine(ctx, line) },
			stdoutch, errch, &wg)
		go handleOutput(
			ctx.stderr, func(line string) string { return makeStderrLine(ctx, line) },
			stderrch, errch, &wg)
	}

	go func() {
		wg.Wait()
		close(stdoutch)
		close(stderrch)
		close(errch)
	}()

	return
}

func rpad(input string, width int) string {
	for i := len(input); i < width; i++ {
		input += " "
	}
	return input
}

func formatPrefix(ctxlist []*cmdContext) {
	maxwidth := 0
	for _, ctx := range ctxlist {
		if len(ctx.prefix) > maxwidth {
			maxwidth = len(ctx.prefix)
		}
	}

	for i, ctx := range ctxlist {
		ctx.prefix = colorize(rpad(ctx.prefix, maxwidth), i)
	}
}

func MergeTail(cmds []TailCmd, writer io.Writer) (err error) {
	ctxlist := make([]*cmdContext, 0, len(cmds))
	var wg sync.WaitGroup

	defer func() {
		for _, ctx := range ctxlist {
			ctx.stdout.Close()
			ctx.stderr.Close()
		}

		// kill running cmds
		for _, tc := range cmds {
			if tc.Cmd.Process != nil {
				tc.Cmd.Process.Kill()
			}
		}

		// wait for all cmds
		wg.Wait()
	}()

	for i, tc := range cmds {
		// prepare pipes
		stdout, pipeErr := tc.Cmd.StdoutPipe()
		if pipeErr != nil {
			err = errors.Wrapf(pipeErr, "failed to get stdout for Cmd: %v", tc.Cmd.Args)
			return
		}
		stderr, pipeErr := tc.Cmd.StderrPipe()
		if pipeErr != nil {
			err = errors.Wrapf(pipeErr, "failed to get stderr for Cmd: %v", tc.Cmd.Args)
			return
		}

		ctxlist = append(ctxlist, &cmdContext{stdout, stderr, tc.Prefix, i})

		// start cmd
		cmdErr := tc.Cmd.Start()
		if cmdErr != nil {
			err = errors.Wrapf(cmdErr, "failed to start Cmd: %v", tc.Cmd.Args)
			return
		} else {
			log.Debugf("started cmd: %v pid: %v", tc.Cmd.Args, tc.Cmd.Process.Pid)

			wg.Add(1)
			// wait cmd and logging
			go func(cmd *exec.Cmd) {
				err := cmd.Wait()
				_, isExitErr := err.(*exec.ExitError)
				if err != nil && !isExitErr {
					log.Errorf("failed to wait on Cmd: %v err: %v", cmd.Args, err)
				} else {
					log.Debugf("Cmd exits: %v %v", cmd.Args, cmd.ProcessState)
				}
				wg.Done()
			}(tc.Cmd)
		}
	}

	formatPrefix(ctxlist)

	errCount := 0
	stdoutch, stderrch, errch := mergeCmds(ctxlist)
	for {
		select {
		case line, ok := <-stdoutch:
			if ok {
				writer.Write(append([]byte(line), '\n'))
			} else {
				stdoutch = nil
			}
		case line, ok := <-stderrch:
			if ok {
				writer.Write(append([]byte(line), '\n'))
			} else {
				stderrch = nil
			}
		case cmdErr, ok := <-errch:
			if ok {
				log.Errorf("error: %v", cmdErr)
				errCount++
				err = errors.Errorf("%d errors encountered", errCount)
			} else {
				errch = nil
			}
		}

		if stdoutch == nil && stderrch == nil && errch == nil {
			break
		}
	}

	return
}
