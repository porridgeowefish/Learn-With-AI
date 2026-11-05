//go:build !windows

package desktop

import "errors"

func installedUpdateSupported() bool { return false }
func readUpdateResult() string       { return "" }
func validateUpdateInstaller(string, string) error {
	return errors.New("当前系统请使用 Gitee 安装包更新")
}
func launchUpdateInstaller(string) error {
	return errors.New("当前系统请使用 Gitee 安装包更新")
}
