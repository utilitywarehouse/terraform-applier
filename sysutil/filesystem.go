package sysutil

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
)

// RemoveAll will remove given dir and sub dir recursively using exec 'rm -rf'
// given dir path should be absolute
func RemoveAll(dir string) error {
	if !path.IsAbs(dir) {
		return fmt.Errorf("dir path needs to be absolute given:%s", dir)
	}

	cmd := exec.Command("rm", "-r", "-f", dir)
	return cmd.Run()
}

// CopyFile copies a file
func CopyFile(src, dst string) error {
	var err error
	var srcFileDescriptor *os.File
	var dstFileDescriptor *os.File
	var srcInfo os.FileInfo

	if srcFileDescriptor, err = os.Open(src); err != nil {
		return err
	}
	defer srcFileDescriptor.Close()

	if dstFileDescriptor, err = os.Create(dst); err != nil {
		return err
	}
	defer dstFileDescriptor.Close()

	if _, err = io.Copy(dstFileDescriptor, srcFileDescriptor); err != nil {
		return err
	}
	if srcInfo, err = os.Stat(src); err != nil {
		return err
	}
	return os.Chmod(dst, srcInfo.Mode())
}

// CopyDir copies a dir recursively
func CopyDir(src string, dst string) error {
	var err error
	var fileDescriptors []os.DirEntry
	var srcInfo os.FileInfo

	if srcInfo, err = os.Stat(src); err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	if fileDescriptors, err = os.ReadDir(src); err != nil {
		return err
	}
	for _, fd := range fileDescriptors {
		srcPath := path.Join(src, fd.Name())
		dstPath := path.Join(dst, fd.Name())

		if fd.Name() == ".git" {
			continue // Exclude .git dir
		}

		if fd.IsDir() {
			if err = CopyDir(srcPath, dstPath); err != nil {
				fmt.Println(err)
			}
		} else {
			if err = CopyFile(srcPath, dstPath); err != nil {
				fmt.Println(err)
			}
		}
	}
	return nil
}
