#!/usr/bin/env python3
# Builds the theme mosaics of the docs from the home screen of every theme:
#   web:     frontend: BASELINE=1 npx playwright test          → .run/visual/baseline/home-<id>.png
#   desktop: the archived Rust app's live test with ADM_LIVE_SHOTS (archive/desktop-rust/README.md), its live-theme-<id>.png copied
#            into .run/desk-themes-rs/
# Writes docs/screenshots/themes.jpg (web, two per row) and docs/img/conformity/web-desktop-{1,2,3}.jpg (web beside
# desktop, six themes each). Needs Pillow.
import os
from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WEB = os.path.join(ROOT, '.run/visual/baseline/home-{}.png')
DESK = os.path.join(ROOT, '.run/desk-themes-rs/live-theme-{}.png')
THEMES = [('default', 'Default'), ('admanager', 'ADManager'), ('breeze', 'Breeze'), ('breeze-dark', 'Breeze (dark)'),
          ('violet', 'Violet'), ('violet-dark', 'Violet (dark)'), ('aurora', 'Aurora'), ('aurora-dark', 'Aurora (dark)'),
          ('crystal', 'Crystal'), ('crystal-dark', 'Crystal (dark)'), ('cone', 'Cone (dark)'),
          ('classic-blue', 'Classic blue (XP)'), ('classic-gray', 'Classic gray (98)'), ('graphite', 'Graphite (dark)'),
          ('grafana', 'Grafana (dark)'), ('rabbitmq', 'RabbitMQ'),
          ('matrix', 'Matrix (dark)'), ('winamp', 'Winamp (dark)'), ('pink-panther', 'Pink Panther'),
          ('high-contrast', 'High contrast (dark)')]
LABEL = 26


def font(size):
    for f in ('/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf', '/usr/share/fonts/TTF/DejaVuSans-Bold.ttf'):
        if os.path.exists(f):
            return ImageFont.truetype(f, size)
    return ImageFont.load_default()


def tile(path, width):
    im = Image.open(path).convert('RGB')
    return im.resize((width, round(im.height * width / im.width)), Image.LANCZOS)


def grid(cells, cols, width, out, quality):
    """cells: [(label, [paths])]; each cell is its images side by side under a label."""
    f = font(17)
    rendered = []
    for label, paths in cells:
        w = (width - 8 * (len(paths) - 1)) // len(paths)
        ims = [tile(p, w) for p in paths]
        h = max(i.height for i in ims)
        c = Image.new('RGB', (width, h + LABEL), 'white')
        ImageDraw.Draw(c).text((4, 3), label, fill='black', font=f)
        x = 0
        for i in ims:
            c.paste(i, (x, LABEL))
            x += i.width + 8
        rendered.append(c)
    gap = 8
    rows = [rendered[i:i + cols] for i in range(0, len(rendered), cols)]
    H = sum(max(c.height for c in r) for r in rows) + gap * (len(rows) + 1)
    m = Image.new('RGB', (cols * width + gap * (cols + 1), H), 'white')
    y = gap
    for r in rows:
        x = gap
        for c in r:
            m.paste(c, (x, y))
            x += width + gap
        y += max(c.height for c in r) + gap
    os.makedirs(os.path.dirname(out), exist_ok=True)
    m.save(out, quality=quality, optimize=True)
    print(out, m.size)


grid([('%s (%s)' % (label, tid), [WEB.format(tid)]) for tid, label in THEMES], 2, 1200,
     os.path.join(ROOT, 'docs/screenshots/themes.jpg'), 92)
for n, part in enumerate((THEMES[:7], THEMES[7:14], THEMES[14:]), 1):
    grid([('%s — web × desktop' % tid, [WEB.format(tid), DESK.format(tid)]) for tid, _ in part], 1, 1384,
         os.path.join(ROOT, 'docs/img/conformity/web-desktop-%d.jpg' % n), 90)
