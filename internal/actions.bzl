# Copyright Jay Conrod. All rights reserved.

# This file is part of rules_go_simple. Use of this source code is governed by
# the 3-clause BSD license that can be found in the LICENSE.txt file.

"""Common functions for creating actions to build Go programs.

Rules should determine input and output files and providers, but they should
call functions to create actions. This allows action code to be shared
by multiple rules.
"""

load("@bazel_skylib//lib:shell.bzl", "shell")

def declare_archive(ctx, importpath):
    """Declares a new .a file the compiler should produce.

    .a files are consumed by the compiler (for dependency type information)
    and the linker. Both tools locate archives using lists of search paths.
    Archives must be named according to their importpath. For example,
    library "example.com/foo" must be named "<searchdir>/example.com/foo.a".

    Args:
        ctx: analysis context.
        importpath: the name by which the library may be imported.
    Returns:
        A File that should be written by the compiler.
    """
    raw = "{name}%/{importpath}.a".format(
        name = ctx.label.name,
        importpath = importpath,
    )
    parts = raw.split("/")
    if len(parts) > 1:
        last = len(parts) - 1
        parts[last] = "lib" + parts[last]
        raw = "/".join(parts)

    return ctx.actions.declare_file(raw)

# def _search_dir(info):
#     """Returns a directory that should be searched.

#     This directory is passed to the compiler or linker with the -I and -L flags,
#     respectively, to find the archive file for a library. The archive
#     must have been declared with declare_archive.

#     Args:
#         info: GoLibraryInfo.info for this library.
#     Returns:
#         A path string for the directory.
#     """
#     suffix_len = len("/" + info.importpath + ".a")
#     return info.archive.path[:-suffix_len]

def go_compile(ctx, srcs, out, lib, extra_objs = [], importpath = "", deps = []):
    """Compiles a single Go package from sources.

    Args:
        ctx: analysis context.
        srcs: list of source Files to be compiled.
        out: where the compiled result of the go source should be placed (usually _go.o_)
        lib: output .a file. Should have the importpath as a suffix (except the lib),
            for example, library "example.com/foo" should have the path
            "somedir/example.com/libfoo.a".
        extra_objs: objects to be added to the archive,
        importpath: the path other libraries may use to import this package.
        deps: list of GoLibraryInfo objects for direct dependencies.
    """
    args = ctx.actions.args()
    args.add("compile")
    args.add("-stdimportcfg", ctx.file._stdimportcfg)
    dep_infos = [d for d in deps]
    filtered_dep_infos = []
    extra_inputs = []
    for d in dep_infos:
        if d.info.importpath == "":
            extra_inputs.append(d.info.archive)
            continue
        if not d.info.archive.path.endswith(".o"):
            filtered_dep_infos.append(d)
            # no need to add to extra_inputs, it should be in extra_objs

    args.add_all(filtered_dep_infos, before_each = "-arc", map_each = _format_arc)  ### was transitive deps
    if importpath:
        args.add("-p", importpath)
    if len(extra_objs) > 0:
        args.add_joined("-a", extra_objs, join_with = ",")
    args.add("-o", out)
    args.add("-l", lib)
    args.add_all(srcs)

    inputs = srcs + [dep.info.archive for dep in filtered_dep_infos] + [ctx.file._stdimportcfg] + extra_inputs + extra_objs
    ctx.actions.run(
        outputs = [out, lib],
        inputs = inputs,
        executable = ctx.executable._builder,
        arguments = [args],
        mnemonic = "GoCompile",
        use_default_shell_env = True,
    )

def parigot_link(ctx, out, main, linker_script = "", extra_objs = [], deps = []):
    """Links a Go executable for parigot.

    Args:
        ctx: analysis context.
        out: output executable file.
        main: archive file for the main package.
        extra_objs: additional objects to add the binary.
        linker_script: passed to the link stage with -T, usually used with a parigot link.
        deps: list of GoLibraryInfo objects for direct dependencies.
    """
    _go_link_impl(ctx, out, main, linker_script, extra_objs, deps, True)

def go_link(ctx, out, main, linker_script, extra_objs = [], deps = []):
    """Links a Go executable.

    Args:
        ctx: analysis context.
        out: output executable file.
        main: archive file for the main package.
        extra_objs: additional objects to add the binary.
        deps: list of GoLibraryInfo objects for direct dependencies.
        linker_script: passed to the link stage with -T, typically not needed for a normal link.
    """
    _go_link_impl(ctx, out, main, linker_script, extra_objs, deps)

def _go_link_impl(ctx, out, main, linker_script, extra_objs = [], deps = [], parigot_link = False):
    """Links a Go executable, possibly for parigot.

    Args:
        ctx: analysis context.
        out: output executable file.
        main: archive file for the main package.
        extra_objs: additional objects to add the binary.
        deps: list of GoLibraryInfo objects for direct dependencies.
        linker_script: passed to the link stage with -T.
        parigot_link: boolean that should be true for parigot link
    """
    filtered_dep_infos = []
    extra_inputs = []
    for d in [d for d in deps]:
        if d.info.importpath == "":
            extra_inputs.append(d.info.archive)
            continue
        if not (d.info.archive.path.endswith(".o")):
            filtered_dep_infos.append(d)
            # no need to add to extra_inputs,its already in extra_objs

    inputs = [main, ctx.file._stdimportcfg] + [d.info.archive for d in filtered_dep_infos] + extra_inputs + extra_objs

    cmd = "link"
    if parigot_link:
        cmd = "parigot_link"

    args = ctx.actions.args()
    args.add(cmd)

    args.add("-stdimportcfg", ctx.file._stdimportcfg)
    args.add_all(filtered_dep_infos, before_each = "-arc", map_each = _format_arc)  ### was transitive deps
    args.add("-main", main)
    args.add("-o", out)
    if len(extra_objs) > 0:
        args.add("-a", ",".join([o.path for o in extra_objs]))
    if linker_script != "":
        x = type(linker_script)
        script = linker_script
        if x == "Target":
            f = linker_script.files.to_list()
            if len(f) > 1:
                fail("linker script target" + linker_script + " must produce exactly one file")
            script = f[0].path
        elif x == "File":
            script = linker_script.path
        args.add("-T", script)

    mnenomic = "GoLink"
    if parigot_link:
        mnenomic = "ParigotLink"

    ctx.actions.run(
        outputs = [out],
        inputs = inputs,
        executable = ctx.executable._builder,
        arguments = [args],
        mnemonic = mnenomic,
        use_default_shell_env = True,
        env = {
            "GOARCH": "arm64",
        },
    )

def go_build_test(ctx, srcs, deps, out, rundir = "", importpath = ""):
    """Compiles and links a Go test executable.

    Args:
        ctx: analysis context.
        srcs: list of source Files to be compiled.
        deps: list of GoLibraryInfo objects for direct dependencies.
        out: output executable file.
        importpath: import path of the internal test archive.
        rundir: directory the test should change to before executing.
    """
    direct_dep_infos = [d.info for d in deps]
    transitive_dep_infos = depset(transitive = [d.deps for d in deps]).to_list()
    inputs = (srcs +
              [ctx.file._stdimportcfg] +
              [d.archive for d in direct_dep_infos] +
              [d.archive for d in transitive_dep_infos])

    args = ctx.actions.args()
    args.add("test")
    args.add("-stdimportcfg", ctx.file._stdimportcfg)
    args.add_all(direct_dep_infos, before_each = "-direct", map_each = _format_arc)
    args.add_all(transitive_dep_infos, before_each = "-transitive", map_each = _format_arc)
    if rundir != "":
        args.add("-dir", rundir)
    if importpath != "":
        args.add("-p", importpath)
    args.add("-o", out)
    args.add_all(srcs)

    ctx.actions.run(
        outputs = [out],
        inputs = inputs,
        executable = ctx.executable._builder,
        arguments = [args],
        mnemonic = "GoTest",
        use_default_shell_env = True,
    )

def go_build_tool(ctx, srcs, out):
    """Compiles and links a Go executable to be used in the toolchain.

    Only allows a main package that depends on the standard library.
    Does not support data or other dependencies.

    Args:
        ctx: analysis context.
        srcs: list of source Files to be compiled.
        out: output executable file.
    """
    cmd_tpl = ("go tool compile -o {out}.a {srcs} && " +
               "go tool link -o {out} {out}.a")
    cmd = cmd_tpl.format(
        out = shell.quote(out.path),
        srcs = " ".join([shell.quote(src.path) for src in srcs]),
    )
    ctx.actions.run_shell(
        outputs = [out],
        inputs = srcs,
        command = cmd,
        mnemonic = "GoToolBuild",
        use_default_shell_env = True,
    )

def go_write_stdimportcfg(ctx, out):
    """Generates an importcfg file for the standard library.

    importcfg files map import paths to archive files. Every compile and link
    action needs this.

    Args:
        ctx: analysis context.
        out: output importcfg file.
    """
    ctx.actions.run(
        outputs = [out],
        arguments = ["stdimportcfg", "-o", out.path],
        executable = ctx.executable._builder,
        mnemonic = "GoStdImportcfg",
        use_default_shell_env = True,
    )

def _format_arc(lib):
    """Formats a GoLibraryInfo.info object as an -arc argument"""
    return "{}={}".format(lib.info.importpath, lib.info.archive.path)

def go_asm(ctx, srcs, out, includepath = "", importpath = ""):
    """Compiles a set of Go assembler files.

    Args:
        ctx: analysis context.
        srcs: list of source Files to be compiled.
        out: archive for the .o files to be added to.
        includepath: the path to search for .h files used by the assembly code
        importpath: the path other libraries may use to import this package.

    """
    args = ctx.actions.args()
    args.add("asm")
    if importpath:
        args.add("-p", importpath)
    if len(includepath) > 0:
        args.add("-I", includepath)
    args.add("-o", out)
    args.add_all(srcs)

    inputs = srcs
    ctx.actions.run(
        outputs = [out],
        inputs = inputs,
        executable = ctx.executable._builder,
        arguments = [args],
        mnemonic = "GoAsm",
        use_default_shell_env = True,
    )
