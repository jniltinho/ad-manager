// The setup wizard's field rules (src/setupRules.js): what blocks the review and what goes through, per role.
import { expect, test } from 'vite-plus/test'
import { passwordRule, suggestNetbios, validate } from '../../src/setupRules.js'

const base = {
  role: 'new-dc',
  realm: 'empresa.local',
  domain: 'EMPRESA',
  ip: '192.168.1.10',
  src_dc: '',
  dns_dc: '',
  url: 'ldaps://',
  admin: 'Administrator',
  forwarder: '1.1.1.1',
  share_root: '/srv/shares',
  files: true,
  backup: '',
}

test('a complete new domain passes', () => {
  expect(validate(base, 'Senha-forte1', 'Senha-forte1')).toEqual({})
})

test('each wrong field is named', () => {
  const e = validate({ ...base, realm: 'empresa', domain: 'NOME-LONGO-DEMAIS-AQUI', ip: '300.1.1.1' }, 'fraca', 'fraca')
  expect(Object.keys(e).sort()).toEqual(['domain', 'ip', 'password', 'realm'])
})

test('the confirmation must match', () => {
  expect(validate(base, 'Senha-forte1', 'Senha-forte2')).toEqual({ confirm: 'The confirmation does not match.' })
})

test('the roles ask for their own fields', () => {
  expect(Object.keys(validate({ ...base, role: 'dc-join' }, 'x', 'x'))).toEqual(['src_dc'])
  expect(Object.keys(validate({ ...base, role: 'member' }, 'x', 'x'))).toEqual(['dns_dc'])
  expect(Object.keys(validate({ ...base, role: 'restore-dc', domain: '' }, '', ''))).toEqual(['backup'])
  expect(Object.keys(validate({ ...base, role: 'connect', domain: '' }, '', ''))).toEqual(['url'])
  expect(validate({ ...base, role: 'connect', url: 'ldaps://dc01.empresa.local' }, '', '')).toEqual({})
})

test('an existing administrator password only has to be there', () => {
  expect(validate({ ...base, role: 'dc-join', src_dc: '192.168.1.5' }, 'abc', 'abc')).toEqual({})
  expect(validate({ ...base, role: 'dc-join', src_dc: '192.168.1.5' }, '', '')).toEqual({ password: 'Required.' })
})

test('password strength and NetBIOS suggestion', () => {
  expect(passwordRule('Abcdefg1')).toBe('')
  expect(passwordRule('abcdefgh1')).not.toBe('')
  expect(passwordRule('Ab1!')).not.toBe('')
  expect(suggestNetbios('minha-empresa.com.br')).toBe('MINHA-EMPRESA')
  expect(suggestNetbios('a-very-long-company-name.local')).toBe('A-VERY-LONG-COM')
})
