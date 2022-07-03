// Copyright Jay Conrod. All rights reserved.

// This file is part of rules_go_simple. Use of this source code is governed by
// the 3-clause BSD license that can be found in the LICENSE.txt file.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func link(args []string) error {
	return linkImpl(args,false)
}

func parigot_link(args[]string) error {
	return linkImpl(args,true)
}

// link produces an executable file from a main archive file and a list of
// dependencies (both direct and transitive).
func linkImpl(args []string,parigotLink bool) error {
	// Process command line arguments.
	var stdImportcfgPath, mainPath, outPath, extraObjs, linkerScript string
	var archives []archive
	fs := flag.NewFlagSet("link", flag.ExitOnError)
	fs.StringVar(&stdImportcfgPath, "stdimportcfg", "", "path to importcfg for the standard library")
	fs.Var(archiveFlag{&archives}, "arc", "information about dependencies (including transitive dependencies), formatted as packagepath=file (may be repeated)")
	fs.StringVar(&mainPath, "main", "", "path to main package archive file")
	fs.StringVar(&outPath, "o", "", "path to binary file the linker should produce")
	fs.StringVar(&extraObjs, "a", "", "extra args to add to the binary, comma separated")
	fs.StringVar(&linkerScript, "T", "", "passed through to the normal link stage, usually only needed for parigot links")
	fs.Parse(args)

	if len(fs.Args()) != 0 {
		return fmt.Errorf("expected 0 positional arguments; got %d", len(fs.Args()))
	}

	// Build an importcfg file.
	archiveMap, err := readImportcfg(stdImportcfgPath)
	if err != nil {
		return err
	}
	directArchiveMap := make(map[string]string)
	for _, arc := range archives {
		if packageSubstitutionRemoval(arc.packagePath) {
			continue
		}
		directArchiveMap[packageSubstitution(arc.packagePath)] = arc.filePath
		archiveMap[packageSubstitution(arc.packagePath)] = arc.filePath
	}
	importcfgPath, err := writeTempImportcfg(archiveMap)
	if err != nil {
		return err
	}
	defer os.Remove(importcfgPath)

	// Invoke the linker.
	return runLinker(mainPath, importcfgPath, directArchiveMap, outPath,parigotLink, linkerScript)
}

func runLinker(mainPath, _ string, arcs map[string]string, outPath string, parigotLink bool, linkerScript string) error {
	args := []string{"-o",outPath}
	if parigotLink {
		args = append(args, "-nostdlib")
	}
	if linkerScript!="" {
		args = append(args, "-T", linkerScript)
	}
	for _,v:=range arcs{
		parts:=strings.Split(v,"/")
		if len(parts)==1 {
			args = append(args, "-L",fmt.Sprint(v))
		} else{
			args = append(args, "-L",strings.Join(parts[:len(parts)-1],"/"))
		}
	}
	args = append(args, mainPath)
	for _,v:=range arcs{
		parts:=strings.Split(v,"/")
		if len(parts)==1 {
			args = append(args, "-L",fmt.Sprint(v))
		} else{
			n:=parts[len(parts)-1]
			n=strings.TrimPrefix(n,"lib")
			n=strings.TrimSuffix(n,".a")
			args = append(args, "-l"+n)
		}
	}
	msg:="LINK"
	if parigotLink {
		msg ="PARIGOT_LINK"
	}
	fmt.Printf("%s arguments to gccgo %+v\n", msg, args)
	cmd := exec.Command("gccgo", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
