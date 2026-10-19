// Global teardown of the visual tests: after a recording (RECORD=1), replaces the session cookie values in the HARs,
// which are versioned. The names stay (the replay picks the signed-in replies by the cookie header), and the
// redacted value round-trips: the browser gets it from Set-Cookie and sends it back.
import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const redact = (s) => s.replace(/(^|;\s*)([^=;\s]+)=[^;]*/g, (_, sep, name) => `${sep}${name}=redacted`)

export default function scrub() {
  if (!process.env.RECORD) return
  const dir = fileURLToPath(new URL('har/', import.meta.url))
  for (const f of readdirSync(dir).filter((f) => f.endsWith('.har'))) {
    const har = JSON.parse(readFileSync(dir + f, 'utf8'))
    for (const e of har.log.entries) {
      for (const m of [e.request, e.response]) {
        for (const h of m.headers) {
          if (h.name.toLowerCase() === 'cookie') h.value = redact(h.value)
          if (h.name.toLowerCase() === 'set-cookie') h.value = h.value.replace(/^([^=]+)=[^;]*/, '$1=redacted')
        }
        for (const c of m.cookies || []) c.value = 'redacted'
      }
    }
    writeFileSync(dir + f, JSON.stringify(har, null, 2) + '\n')
  }
}
