// Interface language. Source strings are English; translations live in
// locales/<lang>/*.js as { 'English text': 'translated text' } and are merged
// here. t() is reactive: changing lang re-renders every view.
import { ref } from 'vue'

const dictionaries = { 'pt-BR': {} }
// the desktop's own catalog (desktop.js) is added by the desktop build only (src/desktop)
for (const mod of Object.values(import.meta.glob(['./locales/pt-BR/*.js', '!./locales/pt-BR/desktop.js'], { eager: true }))) {
  Object.assign(dictionaries['pt-BR'], mod.default)
}

// addTranslations merges a catalog loaded later (the desktop's)
export const addTranslations = (l, dict) => Object.assign(dictionaries[l], dict)

export const languages = [
  ['pt-BR', 'Português'],
  ['en', 'English'],
]

const STORAGE_KEY = 'ad-manager.lang'
function stored() {
  try {
    return localStorage.getItem(STORAGE_KEY)
  } catch {
    return null
  }
}

// lang is the operator's choice in this browser, else the browser's language (Portuguese or English) until the
// company default arrives.
const browserLang = () => ((globalThis.navigator?.language || '').toLowerCase().startsWith('pt') ? 'pt-BR' : 'en')
export const lang = ref(stored() || browserLang())

// setLang changes the language and remembers the operator's choice.
export function setLang(l) {
  lang.value = l
  try {
    localStorage.setItem(STORAGE_KEY, l)
  } catch {
    // private window or blocked storage: the choice lasts until reload
  }
  document.documentElement.lang = l
}

// useDefault applies the company default unless the operator already chose.
export function useDefault(l) {
  if (!stored() && l) lang.value = l
  document.documentElement.lang = lang.value
}

// t translates an English source string and fills {name} placeholders.
export function t(text, vars) {
  const s = dictionaries[lang.value]?.[text] ?? text
  return vars ? s.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? vars[k] : m)) : s
}

// Server messages are in English; the pt-BR catalog comes from GET /api/i18n
// (internal/i18n, shared with the desktop app and the CLI).
let catalog = { messages: {}, patterns: [] }

// setServerCatalog installs the catalog served by the panel.
export function setServerCatalog(c) {
  catalog = { messages: c?.messages || {}, patterns: (c?.patterns || []).map(([re, tr]) => [new RegExp(re), tr]) }
}

// serverText shows a server message in the interface language: the whole text,
// a pattern, or each ": "-separated part. Unknown parts (LDAP errors, names)
// stay as they are.
export function serverText(s) {
  return !s || lang.value !== 'pt-BR' ? s : toPortuguese(s)
}

function toPortuguese(s) {
  if (catalog.messages[s]) return catalog.messages[s]
  if (dictionaries['pt-BR'][s]) return dictionaries['pt-BR'][s] // the desktop's own messages (Go) are in its catalog
  for (const [re, tr] of catalog.patterns) if (re.test(s)) return s.replace(re, tr)
  const i = s.indexOf(': ')
  return i < 0 ? s : `${toPortuguese(s.slice(0, i))}: ${toPortuguese(s.slice(i + 2))}`
}

// locale is the BCP 47 tag for dates and numbers.
export const locale = () => (lang.value === 'en' ? 'en-US' : 'pt-BR')
