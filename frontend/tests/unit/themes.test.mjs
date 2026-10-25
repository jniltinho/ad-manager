// Theme tests (theme-base): structure and role types of the theme files, color lint of the components, square
// corners, theme registry ↔ CSS, dark themes complete and the contrast matrix.
import { join } from 'node:path'
import { describe, expect, test } from 'vite-plus/test'
import { SRC, contract, contrastOf, darkRequired, files, lint, parseColor, read, themeFile, typeErrors } from './themelib.mjs'

const style = read(join(SRC, 'style.css'))
const { values } = contract(style)
const known = new Set(values.keys())
const themeFiles = files(join(SRC, 'themes')).filter((f) => f.endsWith('.css'))
const blocks = new Map(themeFiles.flatMap((f) => [...themeFile(read(f), known).blocks]))
const merged = (id) => new Map([...values, ...(blocks.get(id) || [])])

// registry of internal/server/themes.go: ids (constants resolved from settings.go) and schemes
const goThemes = read(join(SRC, '../../internal/server/themes.go'))
const goConsts = Object.fromEntries(
  [...read(join(SRC, '../../internal/server/settings.go')).matchAll(/(theme\w+)\s*=\s*"([^"]+)"/g)].map((m) => [m[1], m[2]]),
)
const registry = [...goThemes.matchAll(/\{ID: (\w+|"[^"]+"), Label: "[^"]+", Scheme: "(\w+)"\}/g)].map((m) => ({
  id: m[1].startsWith('"') ? m[1].slice(1, -1) : goConsts[m[1]],
  scheme: m[2],
}))

describe('theme files', () => {
  test('only role declarations under :root[data-theme]', () => {
    for (const f of themeFiles) expect(themeFile(read(f), known).errors, f).toEqual([])
  })
  test('the structure rule catches component rules, unknown roles and corners', () => {
    const bad = themeFile(`.x { color: red } :root[data-theme='a'] { --nope: 1; border-radius: 2px; --color-fg: #000 }`, known)
    expect(bad.errors.length).toBe(3)
    expect(bad.blocks.get('a').get('--color-fg')).toBe('#000')
  })
  test('every role of every theme has the right type', () => {
    expect(typeErrors(values), 'contract').toEqual([])
    for (const { id } of registry) expect(typeErrors(merged(id)), id).toEqual([])
  })
  test('role types catch a gradient in a color role, a color in a background, a bad shadow', () => {
    const bad = new Map([...values, ['--color-fg', 'linear-gradient(red, blue)'], ['--bg-page', '#fff'], ['--shadow-card', 'red']])
    // --color-on-page points to --color-fg, so it is wrong too
    expect(typeErrors(bad).map((e) => e.split(':')[0])).toEqual(['--color-fg', '--color-on-page', '--bg-page', '--shadow-card'])
  })
  test('role types parse gradient stops and shadow colors', () => {
    const bad = new Map([
      ...values,
      ['--bg-page', 'linear-gradient(nope)'],
      ['--bg-tabs', 'linear-gradient(to bottom, #fff 0%, #12345 100%)'],
      ['--shadow-card', '0 0 #12345'],
      ['--shadow-tabs', '0 #000'],
    ])
    expect(typeErrors(bad).map((e) => e.split(':')[0])).toEqual(['--bg-page', '--bg-tabs', '--shadow-card', '--shadow-tabs'])
    const good = new Map([
      ...values,
      ['--bg-page', 'linear-gradient(to bottom in oklab, #fff 0%, rgba(0, 0, 0, 0.5) 100%), none'],
      ['--shadow-card', 'inset 0 -3px 0 #80ba50, 0 1px 2px 0 rgb(0 0 0 / 20%)'],
    ])
    expect(typeErrors(good)).toEqual([])
  })
})

describe('registry', () => {
  test('is read from themes.go with default first', () => {
    expect(registry[0]).toEqual({ id: 'default', scheme: 'light' })
    expect(registry.length).toBeGreaterThan(1)
  })
  test('every theme but the default has a CSS block, every block is registered, every theme file is imported', () => {
    const ids = registry.map((t) => t.id)
    for (const t of ids.slice(1)) expect(blocks.has(t), `no CSS for theme ${t}`).toBe(true)
    for (const b of blocks.keys()) expect(ids, `theme ${b} not in the registry`).toContain(b)
    for (const f of themeFiles) expect(style).toContain(`@import './themes/${f.split('/').pop()}'`)
  })
  test('dark themes redefine every color, background and shadow (no light default leaks)', () => {
    const required = darkRequired(known)
    const missing = (block) => required.filter((r) => !block.has(r))
    for (const t of registry.filter((x) => x.scheme === 'dark')) expect(missing(blocks.get(t.id) || new Map()), t.id).toEqual([])
    // the same check on fixtures: an incomplete dark theme fails, a complete one passes
    expect(missing(new Map([['--color-fg', '#fff']])).length).toBeGreaterThan(50)
    const complete = new Map(required.map((r) => [r, r.startsWith('--color-') ? '#111' : r.startsWith('--bg-') ? 'none' : '0 0 #0000']))
    expect(missing(complete)).toEqual([])
    expect(typeErrors(new Map([...values, ...complete]))).toEqual([])
  })
})

describe('color lint', () => {
  test('components and style.css use roles only', () => {
    const found = files().flatMap((f) => lint(f))
    expect(found.map((f) => `${f.file}:${f.line} ${f.reason}: ${f.value}`)).toEqual([])
  })
  test('catches literal, named and encoded colors, palette utilities and corners; ignores comments and #fragments', () => {
    const vue = `<template>
  <!-- bg-white in a comment -->
  <div class="bg-white text-cn rounded-md rounded-none" :class="ok ? 'text-white' : '!bg-cn-err'" style="color: #ff0000"></div>
  <a href="#abc" id="sec"><svg fill="white"><path style="color: white" /></svg></a>
  <p class="bg-white! border-s-red-500 ring-offset-white" :style="{ color: 'var(--color-cn)' }"></p>
</template>
<script setup>
// '#fff' in a comment
const c = 'rgba(0, 0, 0, 0.5)'
const ok = 'text-fg bg-surface'
const arrow = 'url("data:image/svg+xml,%3Csvg fill=%23113058/%3E")'
</script>
<style>
.x { color: red; background: var(--color-cn); border-radius: 4px; border-radius: 0; border-radius: 0 0 0 0; }
.y { @apply bg-slate-900 text-[#123456] rounded-lg; }
</style>`
    const got = lint(join(SRC, 'fixture.vue'), vue).map((f) => `${f.line} ${f.value}`)
    expect(got).toEqual([
      '3 bg-white',
      '3 text-cn',
      '3 text-white',
      '3 !bg-cn-err',
      '3 #ff0000',
      '3 rounded-md',
      '4 white',
      '4 white',
      '5 bg-white!',
      '5 border-s-red-500',
      '5 ring-offset-white',
      '5 var(--color-cn)',
      '9 rgba(0, 0, 0, 0.5)',
      '11 %23113058',
      '14 red',
      '14 var(--color-cn)',
      '14 border-radius: 4px',
      '15 bg-slate-900',
      '15 text-[#123456]',
      '15 #123456',
      '15 rounded-lg',
    ])
  })

  test('catches single-quoted attributes, color constants and radius in style attributes or objects', () => {
    const vue = `<template>
  <svg fill='white'></svg>
  <p style="border-start-start-radius: 5px !important; border-radius: 0" :style="{ color: ink, borderRadius: '6px' }"></p>
</template>
<script setup>
const ink = 'white'
const box = { borderTopLeftRadius: 0 }
</script>`
    const got = lint(join(SRC, 'fixture.vue'), vue).map((f) => `${f.line} ${f.value}`)
    expect(got).toEqual(['2 white', '3 border-start-start-radius: 5px !important', '3 borderRadius: 6px', '6 white'])
  })
})

describe('folder tabs (four-new-themes)', () => {
  test('the current tab has the content color and a 2–3 px accent bar on top, the others thin separators', () => {
    for (const id of ['cone', 'winamp', 'pink-panther', 'high-contrast']) {
      const m = merged(id)
      expect(m.get('--color-tab-on'), id).toBe(m.get('--color-surface'))
      expect(m.get('--shadow-tab-on'), id).toMatch(/inset 0 [23]px #[0-9a-f]{6}$/)
      expect(m.get('--shadow-tab'), id).not.toBe('0 0 #0000')
    }
  })
})

describe('flat look', () => {
  test('turns off every gradient over any theme (more specific than :root[data-theme])', () => {
    const rule = contract().root.nodes.find((n) => n.selector === ":root[data-flat='true'][data-theme]")
    expect(rule, 'flat rule in style.css').toBeTruthy()
    const set = new Map(rule.nodes.filter((d) => d.type === 'decl').map((d) => [d.prop, d.value]))
    // the select arrow is an icon and the button hover a single color (both stops alike), not gradients
    const keep = ['--bg-select-arrow', '--bg-action-hover']
    for (const r of known) if (r.startsWith('--bg-') && !keep.includes(r)) expect(set.get(r), r).toBe('none')
    for (const r of ['--shadow-btn', '--shadow-card', '--shadow-field', '--shadow-table-head']) expect(set.get(r), r).toBe('none')
  })
})

describe('square corners', () => {
  test('the global rule stays in style.css', () => {
    expect(style).toMatch(/\*,\s*\*::before,\s*\*::after\s*\{\s*border-radius:\s*0 !important;\s*\}/)
  })
})

describe('contrast', () => {
  const matrix = JSON.parse(read(join(SRC, 'themes/matrix.json')))
  test('known ratios, and bad colors fail instead of passing as NaN', () => {
    const v = new Map([
      ['--color-w', '#fff'],
      ['--color-k', '#000'],
      ['--color-w2', 'rgb(100% 100% 100%)'],
      ['--color-bad', 'rgb(1 2)'],
    ])
    expect(contrastOf(v, 'k', ['w'])).toBeCloseTo(21, 5)
    expect(contrastOf(v, 'w', ['w2'])).toBe(1)
    expect(() => contrastOf(v, 'k', ['bad'])).toThrow()
    expect(() => parseColor('rgb(300, 0, 0)')).toThrow()
  })
  for (const { id } of registry) {
    test(`theme ${id}: every recipe passes, or is pending for phase B`, () => {
      // the aaa themes (Cristal): normal text 7:1, large text 4.5:1 (WCAG 1.4.6); components stay at 3:1 (1.4.11)
      const aaa = (matrix.aaa || []).includes(id)
      const min = (r) => (r.size === 'normal' ? (aaa ? 7 : 4.5) : r.size === 'large' ? (aaa ? 4.5 : 3) : 3)
      const zero = (k) => ['0', '0px'].includes((merged(id).get(k) || '').trim())
      const segmented = zero('--subtabs-gap') && zero('--subtab-inset-top') // as archive/desktop-go/internal/themegen
      const failing = matrix.recipes
        .filter((r) => aaa || !r.aaaOnly)
        .filter((r) => segmented || !r.segmentedOnly)
        .filter((r) => contrastOf(merged(id), r.fg, r.bg) < min(r))
        .map((r) => r.id)
        .sort()
      expect(failing).toEqual([...(matrix.pending[id] || [])].sort())
    })
  }
  test('a theme placeholder color (a role) reads on the field', () => {
    for (const { id } of registry) {
      const m = merged(id)
        .get('--placeholder-color')
        .match(/^var\(--color-([a-z0-9-]+)\)$/)
      if (m) expect(contrastOf(merged(id), m[1], ['field']), id).toBeGreaterThanOrEqual(4.5)
    }
  })
  test('every recipe uses known roles', () => {
    for (const r of matrix.recipes) for (const n of [r.fg, ...r.bg]) expect(known.has(`--color-${n}`), `${r.id}: ${n}`).toBe(true)
  })
})
