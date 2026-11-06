#!/usr/bin/env python3
"""Fail if a Mach-O binary links anything but Apple system libraries.

macOS binaries are never fully static: the platform always links libSystem,
and a pure-Go cross-compiled binary additionally references libresolv and the
CoreFoundation/Security frameworks. Every Mac ships those, so they are fine;
a third-party dylib (Homebrew, /opt, an SDK path) is not, because the binary
would then only run on machines that happen to have it.

This exists because the release jobs cross-compile on Linux, where `otool`
is not available.

Usage: python3 scripts/check-macho-dylibs.py <binary> [...]
"""
import re
import struct
import sys

ALLOWED = re.compile(r'^(/usr/lib/|/System/Library/|@rpath/|@loader_path/)')
LC_LOAD_DYLIB = 0xC
LC_LOAD_WEAK_DYLIB = 0x80000018


def check(path):
    data = open(path, 'rb').read()
    magic = struct.unpack('<I', data[:4])[0]
    if magic not in (0xFEEDFACF, 0xFEEDFACE):
        print('::error::%s is not a Mach-O file' % path, file=sys.stderr)
        return False
    is64 = magic == 0xFEEDFACF
    ncmds = struct.unpack('<I', data[16:20])[0]
    off = 32 if is64 else 28
    bad = []
    for _ in range(ncmds):
        if off + 8 > len(data):
            break
        cmd, size = struct.unpack('<II', data[off:off + 8])
        if size < 8:
            break
        if cmd in (LC_LOAD_DYLIB, LC_LOAD_WEAK_DYLIB):
            name_off = struct.unpack('<I', data[off + 8:off + 12])[0]
            end = data.find(b'\0', off + name_off)
            if end < 0:
                off += size
                continue
            lib = data[off + name_off:end].decode('utf-8', 'replace')
            print('  %s' % lib)
            if not ALLOWED.match(lib):
                bad.append(lib)
        off += size
    if bad:
        print('::error::%s links non-system libraries: %s' % (path, ', '.join(bad)),
              file=sys.stderr)
        return False
    return True


def main():
    if len(sys.argv) < 2:
        sys.exit('usage: %s <binary> [...]' % sys.argv[0])
    ok = True
    for path in sys.argv[1:]:
        print('checking %s' % path)
        if not check(path):
            ok = False
    sys.exit(0 if ok else 1)


if __name__ == '__main__':
    main()
