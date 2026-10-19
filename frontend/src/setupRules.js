// Field rules of the setup wizard (pages/Setup.vue), pure so the unit test can run them: validate() returns
// field → English message (the page translates it); an empty object means the plan can go to the review.

const ipv4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/
const dnsName = /^(?=.{3,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?$/i

export const DC_ROLES = ['new-dc', 'dc-join', 'restore-dc']

// needsPassword: the roles that ask for the domain administrator (connect reads an existing AD; restore takes the
// administrator from the backup)
export const needsPassword = (role) => role !== 'connect' && role !== 'restore-dc'

// suggestNetbios turns the first label of the DNS domain into a NetBIOS name: example.local → EXAMPLE
export const suggestNetbios = (realm) =>
  (realm || '')
    .split('.')[0]
    .replace(/[^a-z0-9-]/gi, '')
    .slice(0, 15)
    .toUpperCase()

// passwordRule is Samba's default policy for a new domain: 8+ characters with three of the four kinds
export function passwordRule(p) {
  if (p.length < 8) return 'At least 8 characters.'
  const kinds = [/[a-z]/, /[A-Z]/, /\d/, /[^a-zA-Z\d]/].filter((r) => r.test(p)).length
  return kinds >= 3 ? '' : 'Mix three of: lowercase, uppercase, digits and symbols.'
}

export function validate(plan, password, confirm) {
  const e = {}
  const role = plan.role
  const isDC = DC_ROLES.includes(role)
  if (!dnsName.test(plan.realm || '')) e.realm = 'Use a DNS name with a dot, e.g. company.local.'
  if (role !== 'connect' && role !== 'restore-dc' && !/^[a-z0-9][a-z0-9-]{0,14}$/i.test(plan.domain || ''))
    e.domain = 'Up to 15 letters, digits or hyphens, e.g. COMPANY.'
  if (isDC && !ipv4.test(plan.ip || '')) e.ip = 'An IPv4 address of this server, e.g. 192.168.1.10.'
  if (role === 'restore-dc' && !/^\/.+\.tar\.bz2$/.test(plan.backup || '')) e.backup = 'The full path of the .tar.bz2 file on this server.'
  if (role === 'dc-join' && !ipv4.test(plan.src_dc || '')) e.src_dc = 'The IPv4 address of a DC of the domain.'
  if (role === 'member' && !ipv4.test(plan.dns_dc || '')) e.dns_dc = 'The IPv4 address of a DC of the domain.'
  if ((role === 'new-dc' || role === 'dc-join') && !ipv4.test(plan.forwarder || '')) e.forwarder = 'An IPv4 address, e.g. 1.1.1.1.'
  if ((role === 'member' || (isDC && plan.files)) && !/^\/\S+$/.test(plan.share_root || '')) e.share_root = 'A full path, e.g. /srv/shares.'
  if (role === 'connect' && !/^ldaps?:\/\/[^/\s]+$/.test(plan.url || '')) e.url = 'ldaps:// and the name of the DC.'
  if (needsPassword(role)) {
    if (!(plan.admin || '').trim()) e.admin = 'Required.'
    const p = role === 'new-dc' ? passwordRule(password) : password ? '' : 'Required.'
    if (p) e.password = p
    else if (password !== confirm) e.confirm = 'The confirmation does not match.'
  }
  return e
}
