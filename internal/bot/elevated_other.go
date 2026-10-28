//go:build !windows

package bot

func Elevated() bool { return false }
