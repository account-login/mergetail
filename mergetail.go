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

type TailCmd struct {
	Cmd    *exec.Cmd
	Prefix string
}

type cmdContext struct {
	reader io.ReadCloser
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

func makeLine(ctx *cmdContext, line string) string {
	return ctx.prefix + " " + line
}

func handleCmdContext(ctx *cmdContext, outch chan<- string, errch chan<- error, wg *sync.WaitGroup) {
	scanner := bufio.NewScanner(ctx.reader)
	for scanner.Scan() {
		outch <- makeLine(ctx, scanner.Text())
	}

	err := scanner.Err()
	if err != nil {
		errch <- err
	}
	wg.Done()
}

func mergeCmds(ctxlist []*cmdContext) (outch chan string, errch chan error) {
	outch = make(chan string)
	errch = make(chan error)

	var wg sync.WaitGroup
	wg.Add(len(ctxlist))
	for _, ctx := range ctxlist {
		go handleCmdContext(ctx, outch, errch, &wg)
	}

	go func() {
		wg.Wait()
		close(outch)
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

	defer func() {
		for _, ctx := range ctxlist {
			ctx.reader.Close()
		}
	}()

	for i, tc := range cmds {
		stdout, pipeErr := tc.Cmd.StdoutPipe()
		if pipeErr != nil {
			err = errors.Wrapf(pipeErr, "failed to get stdout for Cmd: %q", tc.Cmd.Path)
			return
		}
		ctxlist = append(ctxlist, &cmdContext{stdout, tc.Prefix, i})

		cmdErr := tc.Cmd.Start()
		if cmdErr != nil {
			err = errors.Wrapf(cmdErr, "failed to start Cmd: %q", tc.Cmd.Path)
			return
		} else {
			log.Debugf("started cmd: %q", tc.Cmd.Path)
		}
	}

	formatPrefix(ctxlist)

	errCount := 0
	outch, errch := mergeCmds(ctxlist)
	for {
		select {
		case line, ok := <-outch:
			if ok {
				buf := []byte(line)
				buf = append(buf, '\n')
				writer.Write(buf)
			} else {
				outch = nil
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

		if outch == nil && errch == nil {
			break
		}
	}

	return
}
