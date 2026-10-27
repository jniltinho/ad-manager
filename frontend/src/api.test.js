import { afterEach, describe, expect, it, vi } from 'vite-plus/test'
import { api, auth, checkUpload, latest, onSessionChange, platform } from './api'

// state is 'resolved', 'rejected' or 'pending' (not settled within a few event-loop turns)
const state = (p) =>
  Promise.race([
    p.then(
      () => 'resolved',
      () => 'rejected',
    ),
    new Promise((r) => setTimeout(() => r('pending'), 20)),
  ])
const res = (status, body, headers = {}) => new Response(body == null ? null : JSON.stringify(body), { status, headers })

// fetchQueue answers each fetch with the next responder; a responder may return a promise held by the test
function fetchQueue(...responders) {
  const calls = []
  globalThis.fetch = vi.fn((url, opts) => {
    calls.push({ url, opts })
    return Promise.resolve(responders.shift()(url, opts))
  })
  return calls
}
function deferred() {
  let resolve
  const promise = new Promise((r) => (resolve = r))
  return { promise, resolve }
}

afterEach(() => {
  platform.maxUpload = 0
  auth.user = null
})

describe('sessions', () => {
  it('sends the generation and drops an answer of an earlier session', async () => {
    const late = deferred()
    const calls = fetchQueue(
      () => late.promise,
      () => res(200, { ok: true }),
    )
    const list = api('GET', '/users')
    await api('POST', '/login', { user: 'a', password: 'b' })
    late.resolve(res(200, [{ sam: 'carol' }]))
    expect(await state(list)).toBe('pending')
    expect(calls[0].opts.headers['X-ADM-Session']).toMatch(/^\d+$/)
  })

  it('keeps the new session when a late 401 of the old one arrives', async () => {
    const late = deferred()
    fetchQueue(
      () => late.promise,
      () => res(200, {}),
    )
    const me = api('GET', '/me')
    await api('POST', '/login', {})
    auth.user = 'administrator'
    late.resolve(res(401, { error: 'sign in' }))
    expect(await state(me)).toBe('pending')
    expect(auth.user).toBe('administrator')
  })

  it("drops what the desktop proxy refuses as stale and adopts the proxy's generation", async () => {
    const calls = fetchQueue(
      () => res(200, {}, { 'X-ADM-Session': '41' }),
      () => res(409, { error: 'stale session' }, { 'X-ADM-Stale': '1' }),
    )
    await api('POST', '/login', {})
    expect(await state(api('GET', '/users'))).toBe('pending')
    expect(calls[1].opts.headers['X-ADM-Session']).toBe('41')
  })

  it('runs the session hooks at sign-in and at sign-out, even a failed one', async () => {
    const hook = vi.fn()
    const off = onSessionChange(hook)
    fetchQueue(
      () => res(200, {}),
      () => Promise.reject(new TypeError('network')),
    )
    await api('POST', '/login', {})
    await expect(api('POST', '/logout', {})).rejects.toThrow('network')
    expect(hook).toHaveBeenCalledTimes(2)
    off()
  })
})

describe('session endings', () => {
  it('drops a late failure of an earlier session too', async () => {
    const late = deferred()
    fetchQueue(
      () => late.promise.then(() => Promise.reject(new TypeError('network'))),
      () => res(200, {}),
    )
    const me = api('GET', '/me')
    await api('POST', '/login', {})
    auth.user = 'administrator'
    late.resolve()
    expect(await state(me)).toBe('pending')
    expect(auth.user).toBe('administrator')
  })

  it('ends the session on a 401 of the current one', async () => {
    const hook = vi.fn()
    const off = onSessionChange(hook)
    fetchQueue(() => res(401, { error: 'sign in' }))
    auth.user = 'administrator'
    // the call belonged to the session it ended: it never settles either (the sign-in screen replaces its page)
    expect(await state(api('GET', '/users'))).toBe('pending')
    expect(auth.user).toBe(null)
    expect(hook).toHaveBeenCalledTimes(1)
    off()
  })
})

describe('latest', () => {
  it('drops a failure that a newer load replaced', async () => {
    const fresh = latest()
    const a = deferred()
    const first = fresh(a.promise.then(() => Promise.reject(new Error('server A down'))))
    await fresh(Promise.resolve('B'))
    a.resolve()
    expect(await state(first)).toBe('pending')
  })

  it('drops an answer that a newer load replaced', async () => {
    const fresh = latest()
    const a = deferred()
    const first = fresh(a.promise)
    const second = fresh(Promise.resolve('B'))
    a.resolve('A')
    expect(await second).toBe('B')
    expect(await state(first)).toBe('pending')
  })
})

describe('checkUpload', () => {
  const form = (...sizes) => {
    const fd = new FormData()
    sizes.forEach((n, i) => fd.append('f' + i, new Blob([new Uint8Array(n)])))
    fd.append('field', 'xy')
    return fd
  }
  it('has no limit on the web', () => expect(() => checkUpload(form(1 << 20))).not.toThrow())
  it('refuses more than the desktop limit before sending, files and fields together', () => {
    platform.maxUpload = 12
    expect(() => checkUpload(form(5, 5))).not.toThrow()
    expect(() => checkUpload(form(5, 6))).toThrow(/MiB/)
  })
})
