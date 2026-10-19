package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	virtualRootDir      = "/home/ubuntu/rootfs" // ホストOS とmount していないパス。mac OS とマウントすると、解凍時にショートカットが不足する。
	rootDir             = "/"
	cgroupDir           = "/sys/fs/cgroup/mycontainer"
	maxChildProcessSize = "20"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("使い方: go run main.go run <コマンド>")
		os.Exit(1)
	}

	// 実行モードによって関数を振り分ける
	switch os.Args[1] {
	case "run":
		invokeParentProcess()
	case "child":
		invokeChildProcess() // プログラムが自分で自分を呼ぶ裏モード
	default:
		panic("不明なコマンド")
	}
}

func invokeParentProcess() {
	if len(os.Args) < 3 || os.Args[1] != "run" {
		fmt.Println("使い方: go run main.go run <コマンド>")
		os.Exit(1)
	}

	containerName := os.Args[2]
	targetCmd := os.Args[3:]

	childInvokingCmd := append([]string{"child", containerName}, targetCmd...)
	selfInvokingCmd := "/proc/self/exe"
	cmd := exec.Command(selfInvokingCmd, childInvokingCmd...)
	//  cmd で作成するプロセスに標準入出力を継承する
	//  inherit standard input and output to new process created by cmd command.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// invoke a process with specifing process attributes.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// OS の仕様に依存する。Linux 用
		// clone システムコールを呼び出す。
		// 1. CLONE_NEWUTS => separate namespace.
		// 2. CLONE_NEWPID => PID:1 としてプロセス ID を分離する。
		// 3. CLONE_NEWNS => separate mount space.
		// 4. CLONE_NEWNET => separate port and ip address.
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS | syscall.CLONE_NEWNET,
	}

	// parent process does not set chroot setings because child process sets it.
	if err := cmd.Start(); err != nil {
		fmt.Printf("エラー: %v\n", err)
		os.Exit(1)
	}

	// make parent process belong to managing cgroup process group.
	cgPath := "/sys/fs/cgroup/" + containerName
	// set cgroup settings.
	os.MkdirAll(cgPath, 0755)
	// restrict  child process resouces.
	os.WriteFile(cgPath+"/pids.max", []byte("20"), 0700)
	pidStr := strconv.Itoa(cmd.Process.Pid)
	// cgroupDir から動的な cgPath に変更
	os.WriteFile(cgPath+"/cgroup.procs", []byte(pidStr), 0700)

	if err := cmd.Wait(); err != nil {
		fmt.Printf("エラー: %v\n", err)
		os.Exit(1)
	}
}

const (
	procMountedDir  = "proc"
	procMountingDir = "/proc"
	procFileSystem  = "proc"
	containersDir   = "/home/ubuntu/containers/"
)

func initOverlayFs(containerName string) string {
	baseRootFs := "/home/ubuntu/rootfs"
	containerDir := "/home/ubuntu/containers/" + containerName

	const (
		OverlayFsUpperDir  = "/upper"
		OverlayFsWorkDir   = "/work"
		OverlayFsMergedDir = "/merged"
	)

	upperDir := containerDir + OverlayFsUpperDir
	workDir := containerDir + OverlayFsWorkDir
	mergedDir := containerDir + OverlayFsMergedDir

	// make the container overlay dir.
	os.MkdirAll(upperDir, 0755)
	os.MkdirAll(workDir, 0755)
	os.MkdirAll(mergedDir, 0755)

	mountOpts := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", baseRootFs, upperDir, workDir)
	if err := syscall.Mount("overlay", mergedDir, "overlay", 0, mountOpts); err != nil {
		fmt.Printf("OverlayFSのマウントに失敗: %v\n", err)
		os.Exit(1)
	}

	return mergedDir
}

func invokeChildProcess() {
	containerName := os.Args[2]
	targetCmd := os.Args[3]
	targetArgs := os.Args[4:]

	containerVirtualRootDir := initOverlayFs(containerName)

	// ターミナルを保持するのは子プロセスのため子プロセスが chroot の設定を行う。
	syscall.Chroot(containerVirtualRootDir)
	os.Chdir(rootDir)

	// syscall.Mount の引数: (マウント元, マウント先, ファイルシステムの種類, フラグ, オプション)
	syscall.Mount(procMountedDir, procMountingDir, procFileSystem, 0, "")

	// 3. 準備が整ったら、目的のコマンド（/bin/sh）を実行！
	cmd := exec.Command(targetCmd, targetArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()

	// 終わってコンテナを抜けるときには、後片付け（マウント解除）をする
	syscall.Unmount(procMountingDir, 0)
}
