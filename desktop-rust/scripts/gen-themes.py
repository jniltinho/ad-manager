#!/usr/bin/env python3
"""Write assets/themes.json from the Fyne app's generated palettes (desktop/themes_gen.go).

themes_gen.go is itself generated from the web panel's theme CSS, so this keeps the three apps on the same colors.
The unit test theme::tests::themes_json_matches_fyne (adm-core) fails when the two get out of step.
Usage: scripts/gen-themes.py [path/to/desktop/themes_gen.go]
"""
import json
import os
import re
import sys

here = os.path.dirname(os.path.abspath(__file__))
src_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(here, "..", "..", "desktop", "themes_gen.go")
out = os.path.join(here, "..", "crates", "core", "assets", "themes.json")
src = open(src_path, encoding="utf-8").read()

order = re.search(r"var themeOrder = \[\]string\{([^}]*)\}", src).group(1)
order = re.findall(r'"([^"]+)"', order)
themes = {}
for m in re.finditer(r'^\t"([a-z0-9-]+)": \{scheme: "(\w+)"(.*?)c: map\[string\]color\.NRGBA\{\n(.*?)\n\t\}\},?$', src, re.S | re.M):
    tid, scheme, head, body = m.groups()
    roles = {}
    for r in re.finditer(r'"([a-z0-9-]+)":\s*\{0x(\w\w), 0x(\w\w), 0x(\w\w), 0x(\w\w)\}', body):
        roles[r.group(1)] = "#" + "".join(r.group(i) for i in range(2, 6))
    min_text = float(re.search(r"minText: ([\d.]+)", head).group(1))
    focus = float(re.search(r"focusRing: ([\d.]+)", head).group(1))
    stripe = float(re.search(r"cardStripe: ([\d.]+)", head).group(1))
    # the current sidebar item's mark: the --shadow-tab-on band along a side (dx 0, dy not 0), if the theme has one
    mark = None
    tab_on = re.search(r"\btabOn: \[\]boxShadow\{((?:\{[^{}]*\{[^{}]*\}\},? ?)*)\}", head)
    if tab_on:
        for sh in re.finditer(r"\{dx: (-?[\d.]+), dy: (-?[\d.]+), c: color\.NRGBA\{0x(\w\w), 0x(\w\w), 0x(\w\w), 0x(\w\w)\}", tab_on.group(1)):
            if float(sh.group(1)) == 0 and float(sh.group(2)) != 0:
                mark = "#" + "".join(sh.group(i) for i in range(3, 7))
                break
    themes[tid] = {"scheme": scheme, "tab_mark": mark, "depthless": "depthless: true" in head, "min_text": min_text, "focus_ring": focus, "card_stripe": stripe,
                   "c": roles}
missing = [t for t in order if t not in themes]
if missing:
    sys.exit(f"themes not parsed: {missing}")
with open(out, "w", encoding="utf-8") as fh:
    json.dump({"order": order, "themes": themes}, fh, indent=1)
    fh.write("\n")
print(f"{len(themes)} themes -> {out}")
