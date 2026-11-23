package auth

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
)

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func stty(tty *os.File, arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = tty
	return cmd.Run()
}

// readHidden prompts on stderr and reads one line from the terminal with echo off (stty), so
// the key is not shown. Echo is restored on return and on Ctrl-C. Without stty (Windows) it
// warns that the input will be visible.
func readHidden(tty *os.File, svc string, stderr io.Writer) (string, error) {
	fmt.Fprintf(stderr, "paste the %s key, then Enter:", svc)
	hidden := false
	if runtime.GOOS == "windows" {
		fmt.Fprintln(stderr, "\nwarning: echo cannot be turned off here; the key will be visible")
	} else if err := stty(tty, "-echo"); err != nil {
		fmt.Fprintln(stderr, "\nwarning: could not turn off echo; the key will be visible")
	} else {
		hidden = true
		sig := make(chan os.Signal, 1)
		done := make(chan struct{})
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		go func() {
			select {
			case <-sig:
				stty(tty, "echo")
				fmt.Fprintln(stderr)
				os.Exit(130)
			case <-done:
			}
		}()
		defer func() {
			signal.Stop(sig)
			close(done)
			stty(tty, "echo")
		}()
	}
	line, err := bufio.NewReader(io.LimitReader(tty, 64<<10)).ReadString('\n')
	if hidden {
		fmt.Fprintln(stderr)
	}
	if err != nil && err != io.EOF {
		return "", err
	}
	return line, nil
}
