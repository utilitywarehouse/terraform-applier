package sysutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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

// HardLinkFile creates hard link if dst doesn't exits
func HardLinkFile(src, dst string) error {
	var err error
	var srcInfo os.FileInfo

	if srcInfo, err = os.Stat(src); err != nil {
		return err
	}

	// skip if dst file already exits
	if dstInfo, err := os.Stat(dst); err == nil {
		if srcInfo.Name() == dstInfo.Name() {
			return nil
		}
	}

	// once plugin binary is downloaded it will not be modified hence
	// we can use hard link instead of copy to save resources and time.
	return os.Link(src, dst)
}

// CopyDirWithHardLinks creates hard links of files from src recursively at dst
func CopyDirWithHardLinks(ctx context.Context, src string, dst string) error {
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

		if fd.IsDir() {
			if err := CopyDirWithHardLinks(ctx, srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := HardLinkFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func IsDirEmpty(path string) (bool, error) {
	dirents, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(dirents) == 0, nil
}

func RemoveDirContentsRecursiveIf(dir string, fn func(path string, fi os.FileInfo) (bool, error)) error {
	var errs []error

	// check if any file/dir needs to be removed from current dir
	if err := RemoveDirContentsIf(dir, fn); err != nil {
		errs = append(errs, err)
	}

	// read current dir and check sub directories
	dirEnts, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, fi := range dirEnts {
		if !fi.IsDir() {
			continue
		}
		p := filepath.Join(dir, fi.Name())
		if err := RemoveDirContentsRecursiveIf(p, fn); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("%s", errs)
	}

	return nil
}

// RemoveDirContentsIf iterated the specified dir and removes entries
// if given function returns true for the given entry
func RemoveDirContentsIf(dir string, fn func(path string, fi os.FileInfo) (bool, error)) error {
	dirEnts, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Save errors until the end.
	var errs []error
	for _, fi := range dirEnts {
		p := filepath.Join(dir, fi.Name())
		stat, err := os.Stat(p)
		if err != nil {
			return err
		}
		if shouldDelete, err := fn(p, stat); err != nil {
			return err
		} else if !shouldDelete {
			continue
		}
		if err := RemoveAll(p); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("%s", errs)
	}
	return nil
}
