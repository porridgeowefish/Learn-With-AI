package httpserver

import (
	"context"
	_ "embed"
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/platform/command"
)

//go:embed workspace_picker.ps1
var windowsPicker string

func pickWorkspaceParent(ctx context.Context) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = command.Context(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", windowsPicker)
	case "darwin":
		cmd = command.Context(ctx, "osascript", "-e", `tell application "System Events"
activate
try
return POSIX path of (choose folder with prompt "Choose a workspace folder")
on error number -128
return ""
end try
end tell`)
	case "linux":
		if bin, err := exec.LookPath("zenity"); err == nil {
			cmd = command.Context(ctx, bin, "--file-selection", "--directory", "--title=Choose a workspace folder")
		} else if bin, err := exec.LookPath("kdialog"); err == nil {
			cmd = command.Context(ctx, bin, "--getexistingdirectory", ".")
		} else {
			return "", errors.New("系统目录选择器不可用，请安装 zenity 或 kdialog")
		}
	default:
		return "", errors.New("当前系统不支持目录选择器")
	}
	data, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if runtime.GOOS == "linux" && errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0 {
			return "", nil
		}
		return "", errors.New("无法打开系统目录选择器")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}
