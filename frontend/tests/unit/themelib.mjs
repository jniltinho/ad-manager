// Helpers of the theme tests (theme-base): the role contract of style.css, the theme files, the role types, the
// color lint of the components and the WCAG contrast of the roles.
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'
import postcss from 'postcss'
import { parse as parseSFC } from '@vue/compiler-sfc'

export const SRC = fileURLToPath(new URL('../../src/', import.meta.url))
export const read = (p) => readFileSync(p, 'utf8')

export function files(dir = SRC, out = []) {
  for (const f of readdirSync(dir)) {
    const p = join(dir, f)
    if (statSync(p).isDirectory()) files(p, out)
    else if (/\.(vue|css|js|mjs)$/.test(f) && !/\.test\.m?js$/.test(f) && !p.includes('/locales/')) out.push(p)
  }
  return out
}

// ── contract ────────────────────────────────────────────────────────────────────────────────────────────────
// The contract is every custom property declared in style.css inside @theme blocks or the :root rule that holds
// the theme parameters (the one with --bg-page).
export function contract(css = read(join(SRC, 'style.css'))) {
  const root = postcss.parse(css)
  const values = new Map()
  const nodes = new Set()
  root.each((n) => {
    const isTheme = n.type === 'atrule' && n.name === 'theme'
    const isParams = n.type === 'rule' && n.selector === ':root' && n.some((d) => d.prop === '--bg-page')
    if (!isTheme && !isParams) return
    nodes.add(n)
    n.walkDecls((d) => d.prop.startsWith('--') && values.set(d.prop, d.value.trim()))
  })
  return { values, nodes, root }
}

// ── theme files ─────────────────────────────────────────────────────────────────────────────────────────────
const THEME_SELECTOR = /^(?::root|html)\[data-theme=(['"])([a-z0-9-]+)\1\]$/

// themeFile parses one themes/*.css: blocks by theme id and the structure errors (only :root[data-theme=id] /
// html[data-theme=id] rules declaring known roles).
export function themeFile(css, known) {
  const blocks = new Map()
  const errors = []
  postcss.parse(css).each((n) => {
    if (n.type === 'comment') return
    const m = n.type === 'rule' && n.selector.match(THEME_SELECTOR)
    if (!m) return errors.push(`line ${n.source.start.line}: only :root[data-theme='id'] { --role: value } rules are allowed`)
    const decls = blocks.get(m[2]) || new Map()
    n.each((d) => {
      if (d.type === 'comment') return
      if (d.type !== 'decl' || !d.prop.startsWith('--'))
        return errors.push(`line ${d.source.start.line}: ${d.prop || d.type} is not a role`)
      if (!known.has(d.prop)) return errors.push(`line ${d.source.start.line}: unknown role ${d.prop}`)
      decls.set(d.prop, d.value.trim())
    })
    blocks.set(m[2], decls)
  })
  return { blocks, errors }
}

// ── color lint ──────────────────────────────────────────────────────────────────────────────────────────────
const PALETTE =
  'slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose'
const PREFIX =
  'bg|text|border(?:-[tlrbxyse])?|from|to|via|ring|ring-offset|inset-ring|outline|fill|stroke|divide|decoration|accent|caret|shadow|inset-shadow|drop-shadow|placeholder'
const UTILITY = new RegExp(
  `(?:^|[\\s"'\`{(,:!])((?:[a-z-]+:)*!?(?:${PREFIX})-(?:white|black|(?:${PALETTE})-\\d{2,3}|cn(?:-[a-z]+)*|\\[(?:#|rgb|hsl|oklch|color:)[^\\]]*\\])(?:\\/\\d+)?!?)(?=[\\s"'\`})\\],]|$)`,
  'g',
)
const LITERAL = /#[0-9a-fA-F]{3,8}\b(?![-\w])|\b(?:rgba?|hsla?|oklch|oklab|lab|lch)\([^)]*\)/g
const NAMED = /(?:^|[\s:(,])(white|black|red|green|blue|gray|grey|silver|yellow|orange|purple|navy|teal)(?=[\s;,)!]|$)/g
// a named color inside a style/fill/stroke attribute of a template
const NAMES = 'white|black|red|green|blue|gray|grey|silver|yellow|orange|purple|navy|teal'
const NAMED_ATTR = new RegExp(`\\b(?:style|fill|stroke|color|stop-color)\\s*=\\s*["'][^"']*?\\b(${NAMES})\\b`, 'g')
// a string that is just a named color (a constant later bound to :style / fill); attr="…" is NAMED_ATTR's
const NAMED_STRING = new RegExp(`(?<=(?<![\\w-]=)['"\`])(${NAMES})(?=['"\`])`, 'g')
// a radius declaration inside a style attribute or a JS style object (border-radius, borderStartStartRadius…)
const RADIUS_DECL = /\b([a-z-]*radius|[a-z]+Radius)\s*:\s*['"]?([^;"'`}\n,]+)/gi
const PALETTE_VAR = /var\(--color-cn[a-z-]*\)/g
const ENCODED = /%23[0-9a-fA-F]{3,8}\b/g // a color inside a data-URL
const RADIUS = /(?:^|[\s"'`:!])(rounded(?:-[a-z]+)?(?:-(?:sm|md|lg|xl|2xl|3xl|4xl|full|\[[^\]]+\]|\([^)]+\)))?)(?=[\s"'`]|$)/g
// attribute values that hold #fragments or ids, not colors
const NON_COLOR_ATTR = /\b(?:href|to|xlink:href|for|id|form|name|aria-[a-z]+)\s*=\s*"[^"]*"/g

const blank = (c) => c.replace(/[^\n]/g, ' ')
const stripJS = (s) => s.replace(/\/\*[\s\S]*?\*\//g, blank).replace(/(^|[^:'"`])\/\/[^\n]*/g, '$1')
const stripHTML = (s) => s.replace(/<!--[\s\S]*?-->/g, blank).replace(NON_COLOR_ATTR, blank)
const isZero = (v) =>
  v
    .replace(/!important/, '')
    .trim()
    .split(/[\s/]+/)
    .every((x) => /^0(px|%|r?em)?$/.test(x))
const isInside = (node, parent) => {
  for (let p = node; p; p = p.parent) if (p === parent) return true
  return false
}

function scan(text, rules, base, out, file) {
  text.split('\n').forEach((l, i) => {
    for (const [re, reason] of rules)
      for (const m of l.matchAll(re)) out.push({ file, line: base + i, value: (m[1] || m[0]).trim(), reason })
  })
}

function lintCSS(css, base, out, file, skip = new Set(), root = postcss.parse(css)) {
  root.walkDecls((d) => {
    if ([...skip].some((n) => isInside(d, n))) return
    const line = base + d.source.start.line - 1
    const v = d.value
    if (d.prop.endsWith('radius') && !isZero(v)) out.push({ file, line, value: `${d.prop}: ${v}`, reason: 'rounded corner' })
    for (const m of v.matchAll(LITERAL)) out.push({ file, line, value: m[0], reason: 'literal color' })
    for (const m of v.matchAll(NAMED)) out.push({ file, line, value: m[1], reason: 'named color' })
    for (const m of v.matchAll(PALETTE_VAR)) out.push({ file, line, value: m[0], reason: 'palette token (use a role)' })
    for (const m of v.matchAll(ENCODED)) out.push({ file, line, value: m[0], reason: 'literal color (data-URL)' })
  })
  root.walkAtRules('apply', (a) => {
    if ([...skip].some((n) => isInside(a, n))) return
    const line = base + a.source.start.line - 1
    for (const m of ` ${a.params} `.matchAll(UTILITY)) out.push({ file, line, value: m[1], reason: 'palette utility (use a role)' })
    for (const m of a.params.matchAll(LITERAL)) out.push({ file, line, value: m[0], reason: 'literal color' })
    for (const m of ` ${a.params} `.matchAll(RADIUS)) out.push({ file, line, value: m[1], reason: 'rounded corner' })
  })
}

// lint returns the color findings of one source file (style.css: everything outside the contract; themes/*.css
// are checked by themeFile and typeErrors instead).
export function lint(path, text = read(path)) {
  const file = relative(SRC, path)
  const out = []
  if (file.startsWith('themes/')) return out
  const code = [
    [UTILITY, 'palette utility (use a role)'],
    [LITERAL, 'literal color'],
    [RADIUS, 'rounded corner'],
    [PALETTE_VAR, 'palette token (use a role)'],
    [ENCODED, 'literal color (data-URL)'],
  ]
  const radiusDecls = (text, base) =>
    text.split('\n').forEach((l, i) => {
      for (const m of l.matchAll(RADIUS_DECL))
        if (!isZero(m[2])) out.push({ file, line: base + i, value: `${m[1]}: ${m[2].trim()}`, reason: 'rounded corner' })
    })
  if (path.endsWith('.css')) {
    if (file === 'style.css') {
      const c = contract(text)
      lintCSS(text, 1, out, file, c.nodes, c.root)
    } else lintCSS(text, 1, out, file)
  } else if (path.endsWith('.vue')) {
    const { descriptor } = parseSFC(text, { filename: file })
    const t = descriptor.template
    if (t) {
      scan(stripHTML(t.content), [...code, [NAMED_ATTR, 'named color'], [NAMED_STRING, 'named color']], t.loc.start.line, out, file)
      radiusDecls(stripHTML(t.content), t.loc.start.line)
    }
    for (const s of [descriptor.script, descriptor.scriptSetup].filter(Boolean)) {
      scan(stripJS(s.content), [...code, [NAMED_STRING, 'named color']], s.loc.start.line, out, file)
      radiusDecls(stripJS(s.content), s.loc.start.line)
    }
    for (const st of descriptor.styles) lintCSS(st.content, st.loc.start.line, out, file)
  } else {
    scan(stripJS(text), [...code, [NAMED_STRING, 'named color']], 1, out, file)
    radiusDecls(stripJS(text), 1)
  }
  return out.filter((f) => !(f.reason === 'rounded corner' && /^rounded(-[a-z]+)?-none$/.test(f.value)))
}

// ── colors and contrast ─────────────────────────────────────────────────────────────────────────────────────
function oklchToRGB(L, C, H) {
  const h = (H * Math.PI) / 180
  const a = C * Math.cos(h)
  const b = C * Math.sin(h)
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3
  const lin = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ]
  return lin.map((x) => 255 * Math.min(1, Math.max(0, x > 0.0031308 ? 1.055 * x ** (1 / 2.4) - 0.055 : 12.92 * x)))
}

// checked rejects NaN and out-of-range channels: a bad value must fail the test, never pass silently
function checked(c, v) {
  if (c.some((x) => !Number.isFinite(x)) || c.slice(0, 3).some((x) => x < 0 || x > 255) || c[3] < 0 || c[3] > 1)
    throw new Error(`bad color ${v}`)
  return c
}

// parseColor turns hex, rgb(a) (numbers or %), oklch or transparent into [r, g, b, a] (0-255, alpha 0-1).
export function parseColor(v) {
  v = v.trim()
  if (v === 'transparent') return [0, 0, 0, 0]
  let m = v.match(/^#([0-9a-f]{3,8})$/i)
  if (m && [3, 4, 6, 8].includes(m[1].length)) {
    let h = m[1]
    if (h.length <= 4) h = [...h].map((c) => c + c).join('')
    const n = [0, 2, 4, 6].map((i) => (i < h.length ? parseInt(h.slice(i, i + 2), 16) : 255))
    return checked([n[0], n[1], n[2], n[3] / 255], v)
  }
  m = v.match(/^rgba?\(([^)]+)\)$/)
  if (m) {
    const p = m[1].split(/[\s,/]+/).filter(Boolean)
    if (p.length !== 3 && p.length !== 4) throw new Error(`bad color ${v}`)
    const ch = p.slice(0, 3).map((x) => (x.endsWith('%') ? (parseFloat(x) * 255) / 100 : Number(x)))
    const a = p[3] === undefined ? 1 : p[3].endsWith('%') ? parseFloat(p[3]) / 100 : Number(p[3])
    return checked([...ch, a], v)
  }
  m = v.match(/^oklch\(([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)\)$/)
  if (m) return checked([...oklchToRGB(m[2] ? m[1] / 100 : +m[1], +m[3], +m[4]), 1], v)
  throw new Error(`unsupported color ${v}`)
}

// resolve follows var() chains in the merged values of a theme.
export function resolve(values, name, depth = 0) {
  const v = values.get(name)
  if (v === undefined) throw new Error(`undefined role ${name}`)
  if (depth > 20) throw new Error(`var() loop at ${name}`)
  return v.replace(/var\((--[a-z0-9-]+)\)/g, (_, n) => resolve(values, n, depth + 1))
}

const over = (fg, bg) => [0, 1, 2].map((i) => fg[i] * fg[3] + bg[i] * (1 - fg[3])).concat(1)
const lum = (c) => {
  const [r, g, b] = c.slice(0, 3).map((x) => {
    x /= 255
    return x <= 0.04045 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}
export const ratio = (a, b) => {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p)
  return (x + 0.05) / (y + 0.05)
}

// contrastOf: the fg role over the bg roles (layered: the last one is the bottom, composited upwards, over white).
export function contrastOf(values, fg, bgs) {
  let bg = [255, 255, 255, 1]
  for (const b of [...bgs].reverse()) bg = over(parseColor(resolve(values, `--color-${b}`)), bg)
  const r = ratio(over(parseColor(resolve(values, `--color-${fg}`)), bg), bg)
  if (!Number.isFinite(r)) throw new Error(`contrast of ${fg} over ${bgs} is not a number`)
  return r
}

// ── role types ──────────────────────────────────────────────────────────────────────────────────────────────
const splitTop = (v) => {
  const parts = []
  let depth = 0
  let cur = ''
  for (const ch of v) {
    if (ch === '(') depth++
    if (ch === ')') depth--
    if (ch === ',' && depth === 0) {
      parts.push(cur.trim())
      cur = ''
    } else cur += ch
  }
  return [...parts, cur.trim()]
}

// a box-shadow layer: optional inset, 2-4 lengths, a color
const SHADOW_PART = /^(?:inset\s+)?((?:-?[\d.]+(?:px)?\s+){1,3}-?[\d.]+(?:px)?)\s+(.+?)(?:\s+inset)?$/i
const isShadow = (v) =>
  v === 'none' ||
  splitTop(v).every((p) => {
    const m = p.match(SHADOW_PART)
    return m && m[1].trim().split(/\s+/).length >= 2 && isColor(m[2])
  })
const isColor = (v) => {
  try {
    parseColor(v)
    return true
  } catch {
    return false
  }
}
// a gradient stop: a color followed by up to two positions
const isStop = (p) => isColor(p.replace(/(\s+-?[\d.]+(%|px|deg|turn|rad)?){1,2}$/, ''))
const GRADIENT = /^(?:repeating-)?(?:linear|radial|conic)-gradient\((.*)\)$/s
// a background image: none, url(…) or gradients (each with at least two color stops), comma separated
const isBackground = (v) =>
  splitTop(v).every((layer) => {
    if (layer === 'none' || /^url\([^()]*\)$/.test(layer)) return true
    const m = layer.match(GRADIENT)
    if (!m) return false
    const args = splitTop(m[1])
    const stops = isStop(args[0]) ? args : args.slice(1)
    return stops.length >= 2 && stops.every(isStop)
  })

// typeErrors checks every role (merged theme over the contract): colors must be colors, --bg-* a background
// image, --shadow-* / --bevel-* a box-shadow (comma list of offsets + color).
export function typeErrors(values) {
  const errors = []
  for (const name of values.keys()) {
    let v
    try {
      v = resolve(values, name)
    } catch (e) {
      errors.push(`${name}: ${e.message}`)
      continue
    }
    if (name.startsWith('--color-') && !isColor(v)) errors.push(`${name}: not a color (${v})`)
    else if (name.startsWith('--bg-') && !isBackground(v)) errors.push(`${name}: not a background image (${v})`)
    else if (/^--(shadow|bevel)-/.test(name) && !isShadow(v)) errors.push(`${name}: not a box-shadow (${v})`)
  }
  return errors
}

// darkRequired: what a dark theme must redefine itself (the contract defaults are light).
export const darkRequired = (known) =>
  [...known].filter((r) => (r.startsWith('--color-') && !r.startsWith('--color-cn')) || r.startsWith('--bg-') || r.startsWith('--shadow-'))
