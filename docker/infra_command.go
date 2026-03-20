package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isRoot 检测当前进程是否以 root 身份运行
func isRoot() bool {
	return os.Getuid() == 0
}

// runCommand 执行命令并返回 stdout 输出
// 如果命令执行失败，返回空字符串和错误
func runCommand(args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("runCommand: 参数不能为空")
	}
	cmd := exec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if debug {
			fmt.Printf("[CMD] 命令失败: %s => %v\n", strings.Join(args, " "), err)
		}
		return strings.TrimSpace(string(out)), err
	}
	return strings.TrimSpace(string(out)), nil
}

// runCommandOutput 执行命令并分别返回 stdout 和 stderr
func runCommandOutput(args ...string) (stdout, stderr string, err error) {
	if len(args) == 0 {
		return "", "", fmt.Errorf("runCommandOutput: 参数不能为空")
	}
	cmd := exec.Command(args[0], args[1:]...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), err
}

// runCommandSudo 以 sudo 权限执行命令
// 如果当前已经是 root 用户，则跳过 sudo 以避免不必要的 PAM 认证开销
func runCommandSudo(args ...string) (string, error) {
	if isRoot() {
		return runCommand(args...)
	}
	sudoArgs := append([]string{"sudo"}, args...)
	return runCommand(sudoArgs...)
}

// runCommandSudoSilent 以 sudo 权限执行命令（静默模式），只关心是否成功
// 如果当前已经是 root 用户，则跳过 sudo
func runCommandSudoSilent(args ...string) error {
	if isRoot() {
		return runCommandSilent(args...)
	}
	sudoArgs := append([]string{"sudo"}, args...)
	return runCommandSilent(sudoArgs...)
}

// runCommandSilent 执行命令，忽略输出，只关心是否成功
func runCommandSilent(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("runCommandSilent: 参数不能为空")
	}
	cmd := exec.Command(args[0], args[1:]...)
	return cmd.Run()
}

// commandExists 检查命令是否存在
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}