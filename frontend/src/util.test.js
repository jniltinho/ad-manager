import { expect, test } from 'vite-plus/test'
import { generatePassword, daysSince } from './util.js'
import { lang, t } from './i18n.js'

test('generatePassword has the 4 character sets and the requested length', () => {
  for (let i = 0; i < 200; i++) {
    const s = generatePassword(14)
    expect(s).toHaveLength(14)
    expect(s).toMatch(/[A-Z]/)
    expect(s).toMatch(/[a-z]/)
    expect(s).toMatch(/[2-9]/)
    expect(s).toMatch(/[@#%+=!?]/)
    expect(s).not.toMatch(/[0O1lI]/)
  }
  expect(generatePassword(30)).toHaveLength(30)
})

test('daysSince without a date is infinite', () => {
  expect(daysSince('')).toBe(Infinity)
})

test('t translates to Portuguese, keeps English and fills placeholders', () => {
  lang.value = 'pt-BR'
  expect(t('Cancel')).toBe('Cancelar')
  expect(t('Password of {user} reset.', { user: 'ana' })).toBe('Senha de ana redefinida.')
  lang.value = 'en'
  expect(t('Cancel')).toBe('Cancel')
  expect(t('Password of {user} reset.', { user: 'ana' })).toBe('Password of ana reset.')
})
