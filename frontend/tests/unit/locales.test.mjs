// The pt-BR files merge into one dictionary (i18n.js): a key repeated with another translation would silently change
// the text of every screen that uses it.
import { readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { expect, test } from 'vite-plus/test'

test('a key has the same translation in every locale file', async () => {
  const dir = fileURLToPath(new URL('../../src/locales/pt-BR/', import.meta.url))
  const seen = {}
  const conflicts = []
  for (const f of readdirSync(dir).filter((f) => f.endsWith('.js'))) {
    const { default: dict } = await import(dir + f)
    for (const [k, v] of Object.entries(dict)) {
      if (k in seen && seen[k].v !== v) conflicts.push(`${k}: ${seen[k].f} "${seen[k].v}" × ${f} "${v}"`)
      seen[k] = { f, v }
    }
  }
  expect(conflicts).toEqual([])
})
