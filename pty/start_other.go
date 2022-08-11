//go:build !windows
// +build !windows

package pty

import (
	"log"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/u-root/u-root/pkg/termios"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/xerrors"
)

// From https://github.com/tailscale/tailscale/blob/main/ssh/tailssh/incubator.go.
// opcodeShortName is a mapping of SSH opcode
// to mnemonic names expected by the termios package.
// These are meant to be platform independent.
var opcodeShortName = map[uint8]string{
	gossh.VINTR:         "intr",
	gossh.VQUIT:         "quit",
	gossh.VERASE:        "erase",
	gossh.VKILL:         "kill",
	gossh.VEOF:          "eof",
	gossh.VEOL:          "eol",
	gossh.VEOL2:         "eol2",
	gossh.VSTART:        "start",
	gossh.VSTOP:         "stop",
	gossh.VSUSP:         "susp",
	gossh.VDSUSP:        "dsusp",
	gossh.VREPRINT:      "rprnt",
	gossh.VWERASE:       "werase",
	gossh.VLNEXT:        "lnext",
	gossh.VFLUSH:        "flush",
	gossh.VSWTCH:        "swtch",
	gossh.VSTATUS:       "status",
	gossh.VDISCARD:      "discard",
	gossh.IGNPAR:        "ignpar",
	gossh.PARMRK:        "parmrk",
	gossh.INPCK:         "inpck",
	gossh.ISTRIP:        "istrip",
	gossh.INLCR:         "inlcr",
	gossh.IGNCR:         "igncr",
	gossh.ICRNL:         "icrnl",
	gossh.IUCLC:         "iuclc",
	gossh.IXON:          "ixon",
	gossh.IXANY:         "ixany",
	gossh.IXOFF:         "ixoff",
	gossh.IMAXBEL:       "imaxbel",
	gossh.IUTF8:         "iutf8",
	gossh.ISIG:          "isig",
	gossh.ICANON:        "icanon",
	gossh.XCASE:         "xcase",
	gossh.ECHO:          "echo",
	gossh.ECHOE:         "echoe",
	gossh.ECHOK:         "echok",
	gossh.ECHONL:        "echonl",
	gossh.NOFLSH:        "noflsh",
	gossh.TOSTOP:        "tostop",
	gossh.IEXTEN:        "iexten",
	gossh.ECHOCTL:       "echoctl",
	gossh.ECHOKE:        "echoke",
	gossh.PENDIN:        "pendin",
	gossh.OPOST:         "opost",
	gossh.OLCUC:         "olcuc",
	gossh.ONLCR:         "onlcr",
	gossh.OCRNL:         "ocrnl",
	gossh.ONOCR:         "onocr",
	gossh.ONLRET:        "onlret",
	gossh.CS7:           "cs7",
	gossh.CS8:           "cs8",
	gossh.PARENB:        "parenb",
	gossh.PARODD:        "parodd",
	gossh.TTY_OP_ISPEED: "tty_op_ispeed",
	gossh.TTY_OP_OSPEED: "tty_op_ospeed",
}

func startPty(ptyReq *ssh.Pty, cmd *exec.Cmd) (oPty PTY, proc Process, err error) {
	ptty, tty, err := pty.Open()
	if err != nil {
		return nil, nil, xerrors.Errorf("open: %w", err)
	}
	closePty := func() {
		_ = ptty.Close()
		_ = tty.Close()
	}
	defer func() {
		if err != nil {
			closePty()
		}
	}()

	if ptyReq != nil {
		// From https://github.com/tailscale/tailscale/blob/main/ssh/tailssh/incubator.go.
		ptyRawConn, err := tty.SyscallConn()
		if err != nil {
			return nil, nil, xerrors.Errorf("SyscallConn: %w", err)
		}
		var ctlErr error
		if err := ptyRawConn.Control(func(fd uintptr) {
			// Load existing PTY settings to modify them & save them back.
			tios, err := termios.GTTY(int(fd))
			if err != nil {
				ctlErr = xerrors.Errorf("GTTY: %w", err)
				return
			}

			// Set the rows & cols to those advertised from the ptyReq frame
			// received over SSH.
			tios.Row = ptyReq.Window.Height
			tios.Col = ptyReq.Window.Width

			for c, v := range ptyReq.Modes {
				if c == gossh.TTY_OP_ISPEED {
					tios.Ispeed = int(v)
					continue
				}
				if c == gossh.TTY_OP_OSPEED {
					tios.Ospeed = int(v)
					continue
				}
				k, ok := opcodeShortName[c]
				if !ok {
					log.Printf("unknown opcode: %d", c)
					continue
				}
				if _, ok := tios.CC[k]; ok {
					tios.CC[k] = uint8(v)
					continue
				}
				if _, ok := tios.Opts[k]; ok {
					tios.Opts[k] = v > 0
					continue
				}
				log.Printf("unsupported opcode: %v(%d)=%v", k, c, v)
			}

			// Save PTY settings.
			if _, err := tios.STTY(int(fd)); err != nil {
				ctlErr = xerrors.Errorf("STTY: %w", err)
				return
			}
		}); err != nil {
			return nil, nil, xerrors.Errorf("ptyRawConn.Control: %w", err)
		}
		if ctlErr != nil {
			return nil, nil, xerrors.Errorf("ptyRawConn.Control func: %w", ctlErr)
		}
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
	}
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.Stdin = tty
	err = cmd.Start()
	if err != nil {
		if runtime.GOOS == "darwin" && strings.Contains(err.Error(), "bad file descriptor") {
			// MacOS has an obscure issue where the PTY occasionally closes
			// before it's used. It's unknown why this is, but creating a new
			// TTY resolves it.
			closePty()
			return startPty(ptyReq, cmd)
		}
		return nil, nil, xerrors.Errorf("start: %w", err)
	}
	oPty = &otherPty{
		pty: ptty,
		tty: tty,
	}
	oProcess := &otherProcess{
		pty:     ptty,
		cmd:     cmd,
		cmdDone: make(chan any),
	}
	go oProcess.waitInternal()
	return oPty, oProcess, nil
}
