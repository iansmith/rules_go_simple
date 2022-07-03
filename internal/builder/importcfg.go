// Copyright Jay Conrod. All rights reserved.

// This file is part of rules_go_simple. Use of this source code is governed by
// the 3-clause BSD license that can be found in the LICENSE.txt file.

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// stdImportcfg produces an importcfg file for all the packages in the
// standard library.
func stdImportcfg(args []string) (err error) {
	// Process command line arguments.
	var outPath string
	fs := flag.NewFlagSet("stdimportcfg", flag.ExitOnError)
	fs.StringVar(&outPath, "o", "", "path to standard library importcfg")
	fs.Parse(args)

	// Use "go list" to list the packages and locate the archives.
	cache, err := ioutil.TempDir("", "gocache")
	if err != nil {
		return err
	}
	defer os.RemoveAll(cache)
	os.Setenv("GOCACHE", cache)
	cmd := exec.Command("go", "list", "-json", "std")
	cmd.Stderr = os.Stderr
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("executing go list: %v", err)
	}

	// Construct the importcfg file from "go list" output.
	archiveMap := make(map[string]string)
	type pkg struct {
		Standard           bool
		ImportPath, Target string
	}
	buf := bytes.NewBuffer(data)
	dec := json.NewDecoder(buf)
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("decoding go list output: %v", err)
		}
		if !p.Standard || p.Target == "" {
			continue
		}
		archiveMap[p.ImportPath] = p.Target
	}

	return writeImportcfg(archiveMap, outPath)
}

// readImportcfg parses an importcfg file. It returns a map from package paths
// to archive file paths.
func readImportcfg(importcfgPath string) (map[string]string, error) {
	archiveMap := make(map[string]string)

	data, err := ioutil.ReadFile(importcfgPath)
	if err != nil {
		return nil, err
	}

	// based on parsing code in cmd/link/internal/ld/ld.go
	for lineNum, line := range strings.Split(string(data), "\n") {
		lineNum++ // 1-based
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		var verb, args string
		if i := strings.Index(line, " "); i < 0 {
			verb = line
		} else {
			verb, args = line[:i], strings.TrimSpace(line[i+1:])
		}
		var before, after string
		if i := strings.Index(args, "="); i >= 0 {
			before, after = args[:i], args[i+1:]
		}
		if verb == "packagefile" {
			archiveMap[before] = after
		}
	}

	return archiveMap, nil
}

// writeTempImportcfg writes a temporary importcfg file. The caller is
// responsible for deleting it.
func writeTempImportcfg(archiveMap map[string]string) (string, error) {
	tmpFile, err := ioutil.TempFile("", "importcfg-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if err := writeImportcfg(archiveMap, tmpPath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	return tmpPath, nil
}

func writeImportcfg(archiveMap map[string]string, outPath string) error {
	pkgPaths := make([]string, 0, len(archiveMap))
	for pkgPath := range archiveMap {
		if packageSubstitutionRemoval(pkgPath) {
			continue
		}
		pkgPaths = append(pkgPaths, pkgPath)
	}
	sort.Strings(pkgPaths)

	buf := &bytes.Buffer{}
	for _, pkgPath := range pkgPaths {
		fmt.Fprintf(buf, "packagefile %s=%s\n", packageSubstitution(pkgPath), archiveMap[pkgPath])
	}

	return ioutil.WriteFile(outPath, buf.Bytes(), 0666)
}
