<script setup>
// DNS of the domain, loaded on open: zones, records of a zone or of a name inside it; A, CNAME and PTR can be added
// and deleted (the server refuses the records of the domain controllers, SOA, NS and SRV).
import { Trash2 } from 'lucide-vue-next'
import { ref } from 'vue'
import { auth, get, post, del, enc } from '../../api'
import { toast } from '../../toasts'
import { askConfirm } from '../../dialogs'
import { t } from '../../i18n'
import Modal from '../Modal.vue'
import LoadingNote from '../LoadingNote.vue'

const emit = defineEmits(['close'])

const dns = ref({ zones: [], zone: '', path: [], loading: true })
async function loadZones() {
  try {
    const zones = await get('/dns/zones')
    dns.value.zones = zones
    if (zones.length) loadRecords((zones.find((z) => !msdcs(z.name)) || zones[0]).name, [])
    else dns.value.loading = false
  } catch (e) {
    dns.value = { error: e.message }
  }
}
let dnsSeq = 0 // only the last request fills the dialog
async function loadRecords(zone, path) {
  const d = dns.value
  const seq = ++dnsSeq
  const reverse = d.zones.find((z) => z.name === zone)?.reverse
  if (zone !== d.zone) d.add = { type: reverse ? 'PTR' : 'A', name: '', data: '', ttl: 3600 }
  Object.assign(d, { zone, path, loading: true, error: '' })
  const node = path.length ? path[path.length - 1] : '@'
  try {
    const r = await get(`/dns/records?zone=${enc(zone)}&node=${enc(node)}`)
    if (seq === dnsSeq && dns.value === d) Object.assign(d, { records: r.records, nodes: r.nodes, loading: false })
  } catch (e) {
    if (seq === dnsSeq && dns.value === d) Object.assign(d, { error: e.message, loading: false })
  }
}

const msdcs = (zone) => zone.toLowerCase().startsWith('_msdcs.') // the DCs' locator zone: never edited
const editable = (r) =>
  !msdcs(dns.value.zone) && ['A', 'CNAME', 'PTR'].includes(r.type) && r.name !== '@' && !r.name.split('.').some((l) => l.startsWith('_'))
async function addRecord() {
  const d = dns.value
  try {
    await post('/dns/records', { zone: d.zone, ...d.add, ttl: Number(d.add.ttl) || 3600 })
    toast.success(t('Record {name} added.', { name: d.add.name }))
    d.add = { ...d.add, name: '', data: '' }
    loadRecords(d.zone, d.path)
  } catch (e) {
    toast.error(e.message)
  }
}
async function deleteRecord(r) {
  const d = dns.value
  if (!(await askConfirm(t('Delete the record {name} {type} {data}?', r), { ok: t('Delete') }))) return
  try {
    await del(`/dns/records?zone=${enc(d.zone)}&name=${enc(r.name)}&type=${enc(r.type)}&data=${enc(r.data)}`)
    toast.success(t('Record {name} deleted.', { name: r.name }))
    loadRecords(d.zone, d.path)
  } catch (e) {
    toast.error(e.message)
  }
}
loadZones()
</script>

<template>
  <Modal wide :title="t('DNS of the domain')" :ok="null" @close="emit('close')">
    <p v-if="dns.error" class="mb-2 text-err">{{ dns.error }}</p>
    <div v-if="dns.zones?.length" class="mb-3 flex flex-wrap items-center gap-2">
      <select class="inp w-72" :value="dns.zone" :aria-label="t('Zone')" @change="loadRecords($event.target.value, [])">
        <option v-for="z in dns.zones" :key="z.name" :value="z.name">{{ z.name }}{{ z.reverse ? ' (' + t('reverse') + ')' : '' }}</option>
      </select>
      <span class="text-[12px]">
        <button type="button" class="act px-0" @click="loadRecords(dns.zone, [])">{{ dns.zone }}</button>
        <template v-for="(n, i) in dns.path" :key="n">
          ›
          <button type="button" class="act px-0" @click="loadRecords(dns.zone, dns.path.slice(0, i + 1))">{{ n }}</button>
        </template>
      </span>
    </div>
    <LoadingNote v-if="dns.loading" />
    <template v-else-if="dns.records">
      <table class="grid-t table-fixed">
        <thead>
          <tr>
            <th>{{ t('Name') }}</th>
            <th class="w-20">{{ t('Record type') }}</th>
            <th class="w-20 text-right">TTL</th>
            <th>{{ t('Data') }}</th>
            <th class="w-10"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in dns.records" :key="i">
            <td class="truncate font-mono text-[11px]" :title="r.name">{{ r.name }}</td>
            <td>{{ r.type }}</td>
            <td class="text-right tabular-nums">{{ r.ttl }}</td>
            <td class="truncate font-mono text-[11px]" :title="r.data">{{ r.data }}</td>
            <td class="text-right">
              <button
                v-if="editable(r)"
                type="button"
                class="act px-0"
                :title="t('Delete')"
                :aria-label="t('Delete')"
                @click="deleteRecord(r)"
              >
                <Trash2 :size="13" />
              </button>
            </td>
          </tr>
          <tr v-for="n in dns.nodes" :key="'n' + n.name">
            <td colspan="5">
              <button type="button" class="act px-0" @click="loadRecords(dns.zone, [...dns.path, n.name])">
                {{ n.name }} › {{ t('{n} names below', { n: n.children }) }}
              </button>
            </td>
          </tr>
          <tr v-if="!dns.records.length && !dns.nodes.length">
            <td colspan="5" class="py-3 text-center text-fg-muted">{{ t('No records.') }}</td>
          </tr>
        </tbody>
      </table>
      <form v-if="!msdcs(dns.zone)" class="mt-3 flex flex-wrap items-center gap-2" @submit.prevent="addRecord">
        <select v-model="dns.add.type" class="inp w-20" :aria-label="t('Record type')">
          <option v-for="ty in ['A', 'CNAME', 'PTR']" :key="ty">{{ ty }}</option>
        </select>
        <input v-model="dns.add.name" class="inp w-32" required :placeholder="t('Name')" :aria-label="t('Name')" />
        <span class="text-[12px] text-fg-muted">.{{ dns.zone }}</span>
        <input
          v-model="dns.add.data"
          class="inp w-48"
          required
          :placeholder="dns.add.type === 'A' ? '192.168.10.50' : 'host.' + (auth.domain || 'example.com')"
          :aria-label="t('Data')"
        />
        <input v-model="dns.add.ttl" type="number" min="1" max="604800" class="inp w-20" aria-label="TTL" title="TTL" />
        <button class="btn">{{ t('Add') }}</button>
      </form>
      <p class="mt-2 text-[11px] text-fg-muted">
        {{
          t(
            'With your account, on the domain controller of the panel. Records of the domain controllers, SOA, NS and SRV are not changed here.',
          )
        }}
      </p>
    </template>
  </Modal>
</template>
