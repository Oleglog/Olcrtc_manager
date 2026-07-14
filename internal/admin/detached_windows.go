//go:build windows

package admin

import "os/exec"

func configureDetachedProcess(_ *exec.Cmd) {}
