package pty

import (
	"os/exec"

	"github.com/gliderlabs/ssh"
)

// Start the command in a TTY.  The calling code must not use cmd after passing it to the PTY, and
// instead rely on the returned Process to manage the command/process.
func Start(ptyReq *ssh.Pty, cmd *exec.Cmd) (PTY, Process, error) {
	return startPty(ptyReq, cmd)
}
