package sysutil

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
)

// ListDirs walks the directory tree rooted at the path and adds all non-directory file paths to a []string.
func ListDirs(rootPath string) ([]string, error) {
	var dirs []string
	files, err := ioutil.ReadDir(rootPath)
	if err != nil {
		return dirs, fmt.Errorf("Could not read %s error=(%v)", rootPath, err)
	}

	for _, file := range files {
		if file.IsDir() {
			dirs = append(dirs, filepath.Join(rootPath, file.Name()))
		}
	}
	return dirs, nil
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
	var fileDescriptors []os.FileInfo
	var srcInfo os.FileInfo

	if srcInfo, err = os.Stat(src); err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	if fileDescriptors, err = ioutil.ReadDir(src); err != nil {
		return err
	}
	for _, fd := range fileDescriptors {
		srcPath := path.Join(src, fd.Name())
		dstPath := path.Join(dst, fd.Name())

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
