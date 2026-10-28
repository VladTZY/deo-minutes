package bot

import "golang.org/x/sys/windows"

// Elevated reports whether the app runs as administrator. Edge then
// relaunches itself without admin rights and the bot loses it.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
