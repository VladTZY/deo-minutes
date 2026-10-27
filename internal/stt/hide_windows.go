//go:build windows

package stt

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps whisper-cli from flashing a console window.
func hideWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
