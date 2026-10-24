#!/usr/bin/env python3
"""Seed assets/i18n/pt-BR.json from the Fyne app's catalog (desktop/i18n_pt_*.go).

The Rust app owns its catalog afterwards; rerun to pull new entries (existing ones are kept, Go wins on a conflict).
Usage: scripts/gen-catalog.py [path/to/desktop]
"""
import ast
import glob
import json
import os
import re
import sys

here = os.path.dirname(os.path.abspath(__file__))
desktop = sys.argv[1] if len(sys.argv) > 1 else os.path.join(here, "..", "..", "desktop")
out = os.path.join(here, "..", "crates", "core", "assets", "i18n", "pt-BR.json")

tok = re.compile(r'//[^\n]*|"(?:\\.|[^"\\])*"|`[^`]*`')
cat = {}
for f in sorted(glob.glob(os.path.join(desktop, "i18n_pt_*.go"))):
    src = open(f, encoding="utf-8").read()
    body = src[src.index("addPT(map[string]string{"):]
    strs = []
    for m in tok.finditer(body):
        s = m.group(0)
        if s.startswith("//"):
            continue
        strs.append(s[1:-1] if s.startswith("`") else ast.literal_eval(s))
    if len(strs) % 2:
        sys.exit(f"{f}: odd number of strings")
    cat.update(zip(strs[0::2], strs[1::2]))

old = {}
if os.path.exists(out):
    old = json.load(open(out, encoding="utf-8"))
old.update(cat)
with open(out, "w", encoding="utf-8") as fh:
    json.dump(dict(sorted(old.items())), fh, ensure_ascii=False, indent=1)
    fh.write("\n")
print(f"{len(old)} entries -> {out}")
