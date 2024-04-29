package sysutil

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sync"
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
func CopyFile(src, dst string, withReplace bool) error {
	var err error
	var srcFileDescriptor *os.File
	var dstFileDescriptor *os.File
	var srcInfo os.FileInfo

	if srcInfo, err = os.Stat(src); err != nil {
		return err
	}

	if !withReplace {
		// skip if dst file already exits
		if dstInfo, err := os.Stat(dst); err == nil {
			if srcInfo.Name() == dstInfo.Name() &&
				srcInfo.Size() == dstInfo.Size() {
				return nil
			}
		}
	}

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
	return os.Chmod(dst, srcInfo.Mode())
}

// CopyDir copies a dir recursively
func CopyDir(src string, dst string, withReplace bool) error {
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

	wg := &sync.WaitGroup{}
	errors := make(chan error)

	for _, fd := range fileDescriptors {
		srcPath := path.Join(src, fd.Name())
		dstPath := path.Join(dst, fd.Name())

		if fd.IsDir() {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err = CopyDir(srcPath, dstPath, withReplace); err != nil {
					errors <- err
				}
			}()
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err = CopyFile(srcPath, dstPath, withReplace); err != nil {
					errors <- err
				}
			}()
		}
	}

	go func() {
		wg.Wait()
		close(errors)
	}()

	for err := range errors {
		if err != nil {
			return err
		}
	}

	return nil
}
