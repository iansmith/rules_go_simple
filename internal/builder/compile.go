// Copyright Jay Conrod. All rights reserved.

// This file is part of rules_go_simple. Use of this source code is governed by
// the 3-clause BSD license that can be found in the LICENSE.txt file.

package main

import (
	"flag"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"strings"
)

// compile produces a Go archive file (.a) from a list of .go sources.  This
// function will filter sources using build constraints (OS and architecture
// file name suffixes and +build comments) and will build an importcfg file
// before invoking the Go compiler.
func compile(args []string) error {
	// Process command line arguments.
	var stdImportcfgPath, packagePath, outPath,extraObjs,libPath string
	var archives []archive
	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	fs.StringVar(&stdImportcfgPath, "stdimportcfg", "", "path to importcfg for the standard library")
	fs.Var(archiveFlag{&archives}, "arc", "information about dependencies, formatted as packagepath=file (may be repeated)")
	fs.StringVar(&packagePath, "p", "", "package path for the package being compiled")
	fs.StringVar(&outPath, "o", "", "path go binary that the compiler should produce")
	fs.StringVar(&libPath, "l", "", "path to archive file the compiler should produce")
	fs.StringVar(&extraObjs, "a", "", "extra object files to add to archive, comma separated")
	fs.Parse(args)
	srcPaths := fs.Args()

	// Extract metadata from source files and filter out sources using
	// build constraints.
	srcs := make([]sourceInfo, 0, len(srcPaths))
	filteredSrcPaths := make([]string, 0, len(srcPaths))
	bctx := &build.Default
	for _, srcPath := range srcPaths {
		if srcPath[len(srcPath)-2:len(srcPath)]==".o" {
			filteredSrcPaths = append(filteredSrcPaths, srcPath)
			continue
		}
		if src, err := loadSourceInfo(bctx, srcPath); err != nil {
			return err
		} else if src.match {
			srcs = append(srcs, src)
			filteredSrcPaths = append(filteredSrcPaths, srcPath)
		}
	}
	// Build an importcfg file that maps this package's imports to archive files
	// from the standard library or direct dependencies.
	stdArchiveMap, err := readImportcfg(stdImportcfgPath)
	if err != nil {
		return err
	}

	directArchiveMap := make(map[string]string)
	for _, arc := range archives {
		directArchiveMap[packageSubstitution(arc.packagePath)] = arc.filePath
	}
	archiveMap := make(map[string]string)
	for _, src := range srcs {
		for _, imp := range src.imports {
			switch {
			case imp == "unsafe":
				continue

			case imp == "C":
				return fmt.Errorf("%s: cgo not supported", src.fileName)

			case stdArchiveMap[imp] != "":
				archiveMap[imp] = stdArchiveMap[imp]

			case directArchiveMap[imp] != "":
				archiveMap[imp] = directArchiveMap[imp]

			default:
				return fmt.Errorf("%s: import %q is not provided by any direct dependency", src.fileName, imp)
			}
		}
	}
	importcfgPath, err := writeTempImportcfg(archiveMap)
	if err != nil {
		return err
	}
	defer os.Remove(importcfgPath)

	// Invoke the compiler.
	if err:=runCompiler(packageSubstitution(packagePath), importcfgPath, filteredSrcPaths, outPath , archiveMap); err!=nil {
		return err
	}
	archiveFiles := []string{outPath}
	if len(extraObjs)>0 {
		objs:=strings.Split(extraObjs,",")
		archiveFiles = append(archiveFiles, objs...)
	}
	return runAr(libPath, archiveFiles)

}

func asm(args []string) error {
	// Process command line arguments.	var archives []archive
	var includePath, packagePath, outPath string

	fs := flag.NewFlagSet("compile", flag.ExitOnError)
	fs.StringVar(&packagePath, "p", "", "package path for the package being compiled")
	fs.StringVar(&outPath, "o", "", "path to archive file the compiler should produce")
	fs.StringVar(&includePath, "I", "", "path to search for .h files when assembling")
	fs.Parse(args)

	srcPaths := fs.Args()

	return runAssembler(packageSubstitution(packagePath), srcPaths, outPath, includePath)
}

func runAssembler(packagePath string, srcPaths []string, outPath string, includePath string) error {
	//args := []string{"tool", "asm"}
	var args []string

	args = append(args, "-xassembler-with-cpp")
	args = append(args, "-c")
	if includePath!="" {
		args = append(args, "-I", includePath)
	}
	args = append(args, "-o", outPath)
	if packagePath == "" {
		packagePath="go.welcome"
	}
	converted := strings.Replace(packagePath,".","_0",-1)
	converted = strings.Replace(converted,"/","_1",-1)
	args=append(args, "-D","GOPKGPATH="+converted)
	args = append(args, srcPaths...)
	fmt.Printf("ASM args to gccgo %v\n", args)
	cmd := exec.Command("gccgo", args...)
	cmd.Env = append(os.Environ(),"GOARCH=arm64")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runAr(outputPath string, files []string) error {
	args := []string{"rcD",outputPath}
	args = append(args, files...)
	fmt.Printf("ARCHIVE args to ar %v\n", args)
	cmd := exec.Command("aarch64-linux-gnu-ar", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}


func runCompiler(packagePath, importcfgPath string, srcPaths []string, outPath string, archiveMap map[string]string) error {
	var args []string
	args = append(args, "-c","-g","-gno-record-gcc-switches")
	for _, v:= range archiveMap{
		parts:=strings.Split(v,"%")
		if len(parts)!=2 {
			continue
		}
		args = append(args, "-I",parts[0]+"%")
	}
	args = append(args, "-o", outPath)
	if packagePath != "" {
		args = append(args, "-fgo-pkgpath="+packagePath)
	}
	args = append(args, srcPaths...)

	cmd := exec.Command("gccgo", args...)

	fmt.Printf("COMPILE args to gccgo %v\n", args)
	//cmd.Env = append(os.Environ(),"GOARCH=arm64")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// subs is the raw substitution data with a mapping from our
// package name to the system package name.
var subs = map[string]string {
	"github.com/iansmith/parigot/src/go/runtime":"runtime",
}

// packageSubstitutionRemoval removes candidates from the list
// of known packages because we are going to substitute it later
// with a package of our own.
func packageSubstitutionRemoval(candidate string) bool {
	for _, v:=range subs {
		if candidate==v {
			return true
		}
	}
	return false
}


// packageSubstitution takes a candidate package name and possibly translates it
// to a different package name.  This is useful for creating custom versions of
// the built-in packages.
func packageSubstitution(candidate string) string {
	sub, ok:=subs[candidate]
	if ok {
		return sub
	}
	return candidate
}