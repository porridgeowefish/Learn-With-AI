//go:build windows

package desktop

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"golang.org/x/sys/windows/registry"
)

func installedUpdateSupported() bool {
	if paths.BuildMode != "release" {
		return false
	}
	k, e := registry.OpenKey(registry.CURRENT_USER, `Software\learn-with-ai\Installer`, registry.QUERY_VALUE)
	if e != nil {
		return false
	}
	defer k.Close()
	root, _, e := k.GetStringValue("InstallLocation")
	if e != nil {
		return false
	}
	arch, _, e := k.GetStringValue("Architecture")
	if e != nil || arch != runtime.GOARCH {
		return false
	}
	exe, e := os.Executable()
	if e != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(exe), filepath.Join(root, "learn-with-ai.exe"))
}

func psLiteral(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

func updateRestartArguments(args []string) string {
	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = syscall.EscapeArg(arg)
	}
	return strings.Join(escaped, " ")
}

func updateResultFile() string {
	base, _ := os.UserCacheDir()
	return filepath.Join(base, "learn-with-ai", "updates", "install-result.txt")
}
func readUpdateResult() string {
	b, e := os.ReadFile(updateResultFile())
	if e != nil {
		return ""
	}
	switch strings.TrimSpace(string(b)) {
	case "0":
		return "上次更新安装成功"
	case "3010":
		return "上次更新已安装，请重新启动 Windows 完成更新"
	default:
		return "上次更新未完成，旧版已重新打开。安装结果：" + strings.TrimSpace(string(b))
	}
}

func encodedPS(script string) *exec.Cmd {
	code := utf16.Encode([]rune(script))
	b := make([]byte, len(code)*2)
	for i, c := range code {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

func installerUpgradeCode() string {
	sum := sha256.Sum256([]byte("learn-with-ai:" + runtime.GOARCH + ":upgrade"))
	h := hex.EncodeToString(sum[:])[:32]
	return strings.ToUpper(fmt.Sprintf("{%s-%s-5%s-a%s-%s}", h[:8], h[8:12], h[13:16], h[17:20], h[20:]))
}

func validateUpdateInstaller(file, version string) error {
	f, e := os.Open(file)
	if e != nil {
		return e
	}
	var b [8]byte
	_, e = f.Read(b[:])
	f.Close()
	if e != nil || hex.EncodeToString(b[:]) != "d0cf11e0a1b11ae1" {
		return errors.New("下载内容不是有效的 MSI 安装包")
	}
	// Read the MSI database, without executing package custom actions.
	script := `$ErrorActionPreference='Stop';$w=New-Object -ComObject WindowsInstaller.Installer;$db=$w.OpenDatabase(` + psLiteral(file) + `,0);$q=$db.OpenView("SELECT Value FROM Property WHERE Property='UpgradeCode'");$q.Execute();$r=$q.Fetch();if(!$r -or $r.StringData(1) -ne ` + psLiteral(installerUpgradeCode()) + `){throw 'MSI does not belong to Learn-With-AI'};`
	script += `$q=$db.OpenView("SELECT Value FROM Property WHERE Property='ProductVersion'");$q.Execute();$r=$q.Fetch();if(!$r -or $r.StringData(1) -ne ` + psLiteral(version) + `){throw 'MSI version does not match release'};`
	if out, e := encodedPS(script).CombinedOutput(); e != nil {
		return fmt.Errorf("安装包身份核验失败：%s", strings.TrimSpace(string(out)))
	}
	return nil
}

func launchUpdateInstaller(file string) error {
	if !installedUpdateSupported() {
		return errors.New("当前不是已登记的 MSI 安装版")
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	result := updateResultFile()
	ready := filepath.Join(filepath.Dir(file), "helper-ready")
	log := filepath.Join(filepath.Dir(file), "install.log")
	// Hold a handle to this exact process before waiting; PID reuse cannot target another app.
	script := fmt.Sprintf(`$ErrorActionPreference='Stop';$p=Get-Process -Id %d -ErrorAction Stop;$handle=$p.Handle;[IO.File]::WriteAllText(%s,'ready');$p.WaitForExit();$code=-1;try{$args=@('/i',('"'+%s+'"'),'/passive','/norestart','/l*v',('"'+%s+'"'));$m=Start-Process msiexec.exe -ArgumentList $args -Wait -PassThru -WindowStyle Hidden;$code=$m.ExitCode}catch{};[IO.File]::WriteAllText(%s,[string]$code);Start-Process -FilePath %s;`, os.Getpid(), psLiteral(ready), psLiteral(file), psLiteral(log), psLiteral(result), psLiteral(exe))
	if len(os.Args) > 1 {
		script = strings.TrimSuffix(script, ";") + " -ArgumentList " + psLiteral(updateRestartArguments(os.Args[1:])) + ";"
	}
	cmd := encodedPS(script)
	if e = cmd.Start(); e != nil {
		return e
	}
	go func() { _ = cmd.Wait() }()
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		if _, e := os.Stat(ready); e == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return errors.New("安装交接未就绪，应用保持运行，请重试")
}
