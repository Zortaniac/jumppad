// This fle has been cloned and mofiied from the go source code
// to allow for the use of a custom ignore list and to replace go's standard
// file walker with the facebookgo/symwalk package

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dirhash defines hashes over directory trees.
// These hashes are recorded in go.sum files and in the Go checksum database,
// to allow verifying that a newly-downloaded module has the expected content.
package dirhash

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/facebookgo/symwalk"
	"github.com/ryanuber/go-glob"
)

// DefaultHash is the default hash function used in new go.sum entries.
var DefaultHash Hash = Hash1

// A Hash is a directory hash function.
// It accepts a list of files along with a function that opens the content of each file.
// It opens, reads, hashes, and closes each file and returns the overall directory hash.
type Hash func(files []string, open func(string) (io.ReadCloser, error)) (string, error)

// Hash1 is the "h1:" directory hash function, using SHA-256.
//
// Hash1 is "h1:" followed by the base64-encoded SHA-256 hash of a summary
// prepared as if by the Unix command:
//
//	sha256sum $(find . -type f | sort) | sha256sum
//
// More precisely, the hashed summary contains a single line for each file in the list,
// ordered by sort.Strings applied to the file names, where each line consists of
// the hexadecimal SHA-256 hash of the file content,
// two spaces (U+0020), the file name, and a newline (U+000A).
//
// File names with newlines (U+000A) are disallowed.
func Hash1(files []string, open func(string) (io.ReadCloser, error)) (string, error) {
	h := sha256.New()
	files = append([]string(nil), files...)
	sort.Strings(files)
	for _, file := range files {
		if strings.Contains(file, "\n") {
			return "", errors.New("dirhash: filenames with newlines are not supported")
		}
		r, err := open(file)
		if err != nil {
			return "", err
		}
		hf := sha256.New()
		_, err = io.Copy(hf, r)
		r.Close()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%x  %s\n", hf.Sum(nil), file)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// HashDir returns the hash of the local file system directory dir,
// replacing the directory name itself with prefix in the file names
// used in the hash function.
// optionally a list of files to ignore can be provided as a globbed list
func HashDir(dir, prefix string, hash Hash, ignore ...string) (string, error) {
	files, err := DirFiles(dir, prefix, ignore...)
	if err != nil {
		return "", err
	}
	osOpen := func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(dir, strings.TrimPrefix(name, prefix)))
	}
	return hash(files, osOpen)
}

// DirFiles returns the list of files in the tree rooted at dir,
// replacing the directory name dir with prefix in each name.
// The resulting names always use forward slashes.
// A globbed list of files to ignore can be provided as a variadic argument
func DirFiles(dir, prefix string, ignore ...string) ([]string, error) {
	var ignoredDirectories []string
	var files []string
	dir = filepath.Clean(dir)
	err := symwalk.Walk(dir, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// check that the result is not in the ignore list
		for _, i := range ignore {
			ignore := glob.Glob(i, file)
			if ignore {
				// if we are ignoring a directory, we need to also ignore any children
				// that are in this directory
				if info.IsDir() {
					ignoredDirectories = append(ignoredDirectories, file)
				}

				return nil
			}
		}

		if info.IsDir() {
			return nil
		} else if file == dir {
			return fmt.Errorf("%s is not a directory", dir)
		}

		// check that the file is not in the ignored directories
		for _, i := range ignoredDirectories {
			if strings.HasPrefix(file, i) {
				return nil
			}
		}

		rel := file
		if dir != "." {
			rel = file[len(dir)+1:]
		}
		f := filepath.Join(prefix, rel)
		files = append(files, filepath.ToSlash(f))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// HashZip returns the hash of the file content in the named zip file.
// Only the file names and their contents are included in the hash:
// the exact zip file format encoding, compression method,
// per-file modification times, and other metadata are ignored.
func HashZip(zipfile string, hash Hash) (string, error) {
	z, err := zip.OpenReader(zipfile)
	if err != nil {
		return "", err
	}
	defer z.Close()
	var files []string
	zfiles := make(map[string]*zip.File)
	for _, file := range z.File {
		files = append(files, file.Name)
		zfiles[file.Name] = file
	}
	zipOpen := func(name string) (io.ReadCloser, error) {
		f := zfiles[name]
		if f == nil {
			return nil, fmt.Errorf("file %q not found in zip", name) // should never happen
		}
		return f.Open()
	}
	return hash(files, zipOpen)
}
