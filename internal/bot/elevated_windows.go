package bot

import "golang.org/x/sys/windows"

// Elevated reports whether the app runs as administrator. Chrome and
// Edge then relaunch themselves without admin rights and the bot loses it.
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
