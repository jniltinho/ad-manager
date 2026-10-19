import { expect, test } from 'vite-plus/test'
import { lang, serverText, setServerCatalog } from './i18n.js'
import catalog from './testdata/i18n-pt.json'

// the same catalog the panel serves at /api/i18n (internal/i18n)
setServerCatalog(catalog)

test('serverText translates server messages (English) to pt-BR, part by part', () => {
  lang.value = 'pt-BR'
  const cases = [
    ['invalid user or password', 'usuário ou senha inválidos'],
    ['access denied: domain administrators only', 'acesso negado: apenas administradores do domínio'],
    ['directory unavailable: LDAP Result Code 200 "Network Error"', 'diretório indisponível: LDAP Result Code 200 "Network Error"'],
    ['invalid data: OU name', 'dados inválidos: nome da OU'],
    [
      'invalid input: share name "a/b" (letters, numbers, space . _ -; reserved: global, homes)',
      'entrada inválida: nome de compartilhamento "a/b" (letras, números, espaço . _ -; reservados: global, homes)',
    ],
    ['member ana: group or member missing', 'membro ana: grupo ou membro inexistente'],
    ['enabled: yes → no', 'ativo: sim → não'],
    ['Ana Souza in OU=Vendas', 'Ana Souza em OU=Vendas'],
    ['connect to ldaps://dc1: x509: bad', 'conectar em ldaps://dc1: x509: bad'],
    ['invalid data: theme: default or admanager', 'dados inválidos: tema: default ou admanager'],
    ['theme: default, admanager or graphite', 'tema: default, admanager ou graphite'],
  ]
  for (const [en, pt] of cases) expect(serverText(en)).toBe(pt)
  lang.value = 'en'
  expect(serverText('invalid user or password')).toBe('invalid user or password')
  lang.value = 'pt-BR'
})
