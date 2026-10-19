// Replaces things the browser draws on its own with themed versions:
// - title= tooltips → .theme-tip (title moves to data-tip while the page lives);
// - validation bubbles ("Please fill out this field") → red border + toast;
// - the open list of a <select class="inp">, where the browser cannot theme it (no appearance: base-select).
import { toast } from './toasts'
import { t } from './i18n'

let tip = null
let tipFor = null

// truncated tells whether a "truncate" element hides part of its text; its tip
// would only repeat what is already visible otherwise. scrollWidth alone is not
// enough: Firefox reports no overflow for table cells cut with an ellipsis, so
// the text is also measured with a Range.
function truncated(el) {
  if (!el.classList.contains('truncate') || el.scrollWidth > el.clientWidth + 1) return true
  const range = document.createRange()
  range.selectNodeContents(el)
  const cs = getComputedStyle(el)
  const room = el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)
  return range.getBoundingClientRect().width > room + 1
}

function showTip(el, x, y) {
  if (el.hasAttribute('title')) {
    const text = el.getAttribute('title')
    el.removeAttribute('title') // otherwise the native tooltip shows too
    if (text) {
      el.dataset.tip = text
      if (!el.hasAttribute('aria-label') && !el.textContent.trim()) el.setAttribute('aria-label', text)
    }
  }
  const text = el.dataset.tip
  if (!text || !truncated(el)) return hideTip()
  tip ??= Object.assign(document.createElement('div'), { className: 'theme-tip', role: 'tooltip' })
  tip.textContent = text
  if (!tip.isConnected) document.body.append(tip)
  tipFor = el
  placeTip(x, y)
}

// placeTip puts the tip just below and right of the pointer, flipping to the
// other side near the window edges.
function placeTip(x, y) {
  if (!tip?.isConnected) return
  const d = tip.getBoundingClientRect()
  const left = x + 14 + d.width > innerWidth - 4 ? Math.max(4, x - d.width - 10) : x + 14
  const top = y + 18 + d.height > innerHeight - 4 ? Math.max(4, y - d.height - 10) : y + 18
  // QHD/4K apply CSS zoom to the page (style.css): pointer coordinates are in
  // viewport pixels but left/top of a fixed element are zoomed, so divide.
  const z = parseFloat(getComputedStyle(document.documentElement).zoom) || 1
  Object.assign(tip.style, { left: left / z + 'px', top: top / z + 'px' })
}

function hideTip() {
  tip?.remove()
  tipFor = null
}

let warned = false
function onInvalid(ev) {
  const el = ev.target
  ev.preventDefault() // no native bubble
  el.classList.add('invalid')
  el.addEventListener('input', () => el.classList.remove('invalid'), { once: true })
  if (warned) return // one toast per submit: the first invalid field
  warned = true
  setTimeout(() => (warned = false))
  const label =
    (el.id && document.querySelector(`label[for="${CSS.escape(el.id)}"]`)?.textContent.trim()) ||
    el.getAttribute('aria-label') ||
    el.placeholder ||
    t('Field')
  toast.error(`${label}: ${el.validationMessage}`)
  el.focus()
}

// ── themed select list ──────────────────────────────────────────────────────────────────────────────────────
// Browsers without appearance: base-select (Firefox, older Chrome) draw the open list of a <select> themselves,
// in the system colors. There the select keeps its value and keyboard (arrows while closed), but a click opens
// .select-pop instead: a listbox in the theme roles, in the top layer (above an open modal dialog), placed like
// the suggestions (zoom-aware). Choosing sets the value and fires input/change, so v-model follows.
let pop = null
let popFor = null
let active = -1

function closeSelect() {
  pop?.remove()
  popFor?.setAttribute('aria-expanded', 'false')
  pop = popFor = null
}

function pickSelect(i) {
  const sel = popFor
  closeSelect()
  if (!sel || sel.selectedIndex === i) return sel?.focus()
  sel.selectedIndex = i
  sel.dispatchEvent(new Event('input', { bubbles: true }))
  sel.dispatchEvent(new Event('change', { bubbles: true }))
  sel.focus()
}

function markActive(i) {
  const items = pop.querySelectorAll('li')
  if (!items.length) return
  active = Math.max(0, Math.min(items.length - 1, i))
  items.forEach((li, k) => li.classList.toggle('active', k === active))
  items[active].scrollIntoView({ block: 'nearest' })
}

function openSelect(sel) {
  closeSelect()
  popFor = sel
  pop = document.createElement('ul')
  pop.className = 'select-pop'
  pop.setAttribute('role', 'listbox')
  pop.popover = 'manual'
  ;[...sel.options].forEach((o, i) => {
    const li = document.createElement('li')
    li.textContent = o.textContent
    li.setAttribute('role', 'option')
    li.setAttribute('aria-selected', String(o.selected))
    if (o.disabled) li.setAttribute('aria-disabled', 'true')
    else li.addEventListener('mousedown', (e) => (e.preventDefault(), pickSelect(i)))
    li.addEventListener('mousemove', () => active !== i && markActive(i))
    pop.append(li)
  })
  document.body.append(pop)
  pop.showPopover?.()
  // QHD/4K zoom the page: the rect is in viewport pixels, a fixed element's left/top are zoomed
  const z = parseFloat(getComputedStyle(document.documentElement).zoom) || 1
  const b = sel.getBoundingClientRect()
  const r = { left: b.left / z, top: b.top / z, bottom: b.bottom / z, width: b.width / z }
  const h = innerHeight / z
  const below = h - r.bottom
  const up = below < 200 && r.top > below
  Object.assign(pop.style, {
    left: r.left + 'px',
    minWidth: r.width + 'px',
    maxHeight: Math.min(320, (up ? r.top : below) - 12) + 'px',
    ...(up ? { bottom: h - r.top + 2 + 'px' } : { top: r.bottom + 2 + 'px' }),
  })
  sel.setAttribute('aria-expanded', 'true')
  markActive(sel.selectedIndex)
}

function onSelectKey(e) {
  if (!pop) return
  const keys = { ArrowDown: 1, ArrowUp: -1, PageDown: 8, PageUp: -8 }
  if (e.key in keys) markActive(active + keys[e.key])
  else if (e.key === 'Home') markActive(0)
  else if (e.key === 'End') markActive(Infinity)
  else if (e.key === 'Enter' || e.key === ' ') {
    if (!pop.querySelectorAll('li')[active]?.hasAttribute('aria-disabled')) pickSelect(active)
  } else if (e.key === 'Escape') {
    closeSelect()
    e.target.focus?.()
  } else if (e.key === 'Tab') return closeSelect()
  else return
  e.preventDefault()
  e.stopPropagation() // Esc must not close the dialog around the select
}

function onSelectDown(e) {
  if (pop?.contains(e.target)) return
  const sel = e.target.closest?.('select.inp')
  if (!sel || sel.disabled || sel.multiple || sel.size > 1) return closeSelect()
  e.preventDefault() // no native list
  sel.focus()
  if (popFor === sel) return closeSelect()
  openSelect(sel)
}

// installNativeReplacements wires the document-wide listeners once.
export function installNativeReplacements() {
  if (!CSS.supports('appearance', 'base-select')) {
    document.addEventListener('mousedown', onSelectDown, true)
    document.addEventListener('keydown', onSelectKey, true)
    document.addEventListener('scroll', (e) => pop && !pop.contains(e.target) && closeSelect(), true)
    addEventListener('resize', closeSelect)
  }
  document.addEventListener('mouseover', (e) => {
    const el = e.target.closest?.('[title], [data-tip]')
    if (el && el !== tipFor) showTip(el, e.clientX, e.clientY)
  })
  document.addEventListener('mousemove', (e) => tipFor && placeTip(e.clientX, e.clientY), { passive: true })
  document.addEventListener('mouseout', (e) => {
    if (e.target.closest?.('[data-tip]') && !e.relatedTarget?.closest?.('[data-tip]')) hideTip()
  })
  document.addEventListener('mousedown', hideTip, true)
  document.addEventListener('scroll', hideTip, true)
  document.addEventListener('invalid', onInvalid, true)
}
