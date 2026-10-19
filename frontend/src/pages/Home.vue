<script setup>
import Suggestions from '../components/Suggestions.vue'
import { ChevronRight, Clock, KeyRound, Lock, LockOpen, Monitor, UserCheck, UserX, UsersRound } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth, get, post, enc } from '../api'
import { toast } from '../toasts'
import { formatDate, inactive } from '../util'
import { t } from '../i18n'
import PasswordDialog from '../components/PasswordDialog.vue'
import LoadingNote from '../components/LoadingNote.vue'

const router = useRouter()
const s = ref(null)
const users = ref([])

async function load() {
  try {
    ;[s.value, users.value] = await Promise.all([get('/summary'), get('/users')])
  } catch (e) {
    toast.error(e.message)
  }
}
load()

const inactiveDays = computed(() => auth.settings?.inactive_days || 90)
const locked = computed(() => users.value.filter((u) => u.locked))
const inactiveUsers = computed(() => users.value.filter((u) => u.enabled && inactive(u.last_logon)))
const systems = computed(() => Object.entries(s.value?.os || {}).sort((a, b) => b[1] - a[1]))
const maxOS = computed(() => Math.max(1, ...systems.value.map((x) => x[1])))

// quick task box (idea from ADAC): just the login
const quick = ref('')
const passwordFor = ref(null)
function openPassword() {
  const u = users.value.find((x) => x.sam.toLowerCase() === quick.value.trim().toLowerCase())
  if (!u) return toast.warn(t('Login not found.'))
  passwordFor.value = u
}
async function unlock(sam) {
  try {
    await post(`/users/${enc(sam)}/unlock`)
    toast.success(t('{name} unlocked.', { name: sam }))
    load()
  } catch (e) {
    toast.error(e.message)
  }
}
const weak = computed(() => s.value && (!s.value.policy.complexity || s.value.policy.min_length < 8))

function passwordDone() {
  passwordFor.value = null
  quick.value = ''
  load()
}
</script>

<template>
  <LoadingNote v-if="!s" />
  <div v-else class="space-y-5">
    <section class="grid grid-cols-2 gap-3 md:grid-cols-6">
      <button
        v-for="c in [
          [t('Enabled users'), s.users.enabled, '/users?f=enabled', 'text-ok', UserCheck, 'bg-decor-ok'],
          [t('Disabled'), s.users.disabled, '/users?f=disabled', 'text-fg-muted', UserX, 'bg-decor-muted'],
          [
            t('Locked'),
            s.users.locked,
            '/users?f=locked',
            s.users.locked ? 'text-err' : 'text-fg-muted',
            Lock,
            s.users.locked ? 'bg-decor-err' : 'bg-decor-muted',
          ],
          [
            t('Inactive {n}+ days', { n: inactiveDays }),
            inactiveUsers.length,
            '/users?f=inactive',
            inactiveUsers.length ? 'text-warn' : 'text-fg-muted',
            Clock,
            inactiveUsers.length ? 'bg-decor-warn' : 'bg-decor-muted',
          ],
          [t('Groups'), s.groups, '/groups', 'text-fg-strong', UsersRound, 'bg-decor-strong'],
          [t('Computers'), s.computers, '/computers', 'text-fg-strong', Monitor, 'bg-decor-strong'],
        ]"
        :key="c[2]"
        class="card relative cursor-pointer overflow-hidden p-3 text-left transition hover:-translate-y-0.5 hover:border-card-hover-line hover:shadow-(--shadow-card-hover)"
        @click="router.push(c[2])"
      >
        <span class="absolute inset-x-0 top-0 h-(--card-stripe)" :class="c[5]"></span>
        <component :is="c[4]" :size="38" class="absolute right-2 bottom-2 opacity-15" :class="c[3]" />
        <div class="text-[11px] font-semibold text-fg-muted uppercase">{{ c[0] }}</div>
        <div class="mt-1 text-3xl font-semibold" :class="c[3]">{{ c[1] }}</div>
      </button>
    </section>

    <div class="grid gap-5 md:grid-cols-3">
      <section class="card">
        <div class="card-h">{{ t('Reset password / unlock') }}</div>
        <form class="flex gap-2 p-3" @submit.prevent="openPassword">
          <Suggestions
            v-model="quick"
            :options="users.map((u) => ({ value: u.sam, label: u.name }))"
            :placeholder="t('user login')"
            :aria-label="t('Login')"
          />
          <button class="btn"><KeyRound :size="13" /> {{ t('Open') }}</button>
        </form>
      </section>

      <section class="card md:col-span-2">
        <div class="card-h">{{ t('Domain controllers') }}</div>
        <table class="grid-t border-0">
          <thead>
            <tr>
              <th>{{ t('Name') }}</th>
              <th>{{ t('System') }}</th>
              <th>{{ t('Last logon') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in s.dcs" :key="d.name">
              <td class="font-semibold">{{ d.name }}</td>
              <td>
                <template v-if="d.os">{{ d.os }} {{ d.os_version }}</template
                ><span v-else class="text-fg-muted">{{ t('not reported') }}</span>
              </td>
              <td>{{ formatDate(d.last_logon) }}</td>
            </tr>
          </tbody>
        </table>
        <p class="px-3 py-1.5 text-[11px] text-fg-muted">
          {{ t('Connected to') }} <code>{{ s.server }}</code
          >. {{ t('Replication/SYSVOL are not checked here — use') }} <code>samba-tool drs showrepl</code>.
        </p>
      </section>
    </div>

    <div class="grid gap-5 md:grid-cols-2">
      <section class="card">
        <div class="card-h">{{ t('Alerts') }}</div>
        <ul class="divide-y divide-line text-[12px]">
          <li v-if="weak" class="flex items-center gap-2 px-3 py-2">
            <span class="tag bg-warn-fill text-on-warn-fill">{{ t('policy') }}</span>
            {{
              t(
                s.policy.complexity
                  ? 'Minimum password length of {n} characters.'
                  : 'Minimum password length of {n} characters, no complexity.',
                {
                  n: s.policy.min_length,
                },
              )
            }}
            <router-link to="/password-policy" class="act ml-auto">{{ t('view') }} <ChevronRight :size="12" /></router-link>
          </li>
          <li v-for="u in locked" :key="u.sam" class="flex items-center gap-2 px-3 py-2">
            <span class="tag bg-err-fill text-on-err-fill">{{ t('locked') }}</span
            >{{ u.name }} ({{ u.sam }})
            <button class="act ml-auto" @click="unlock(u.sam)"><LockOpen :size="12" /> {{ t('unlock') }}</button>
          </li>
          <li v-if="inactiveUsers.length" class="flex items-center gap-2 px-3 py-2">
            <span class="tag bg-neutral-fill text-on-neutral-fill">{{ t('inactive') }}</span>
            {{ t('{n} enabled account(s) with no logon for more than {days} days.', { n: inactiveUsers.length, days: inactiveDays }) }}
            <router-link to="/users?f=inactive" class="act ml-auto">{{ t('review') }} <ChevronRight :size="12" /></router-link>
          </li>
          <li v-if="!weak && !locked.length && !inactiveUsers.length" class="px-3 py-2 text-fg-muted">{{ t('Nothing pending.') }}</li>
        </ul>
      </section>

      <section class="card">
        <div class="card-h">{{ t('Operating systems') }}</div>
        <ul class="space-y-1 p-3 text-[12px]">
          <li v-for="[os, n] in systems" :key="os" class="flex items-center gap-2">
            <span class="w-48 truncate" :title="os">{{ os || t('not reported') }}</span>
            <span
              class="h-3 bg-chart-1"
              :class="/XP|2003|2008 Standard/.test(os) ? '!bg-err-fill' : ''"
              :style="{ width: (n / maxOS) * 60 + '%' }"
            ></span>
            <span class="text-fg-muted">{{ n }}</span>
          </li>
        </ul>
      </section>
    </div>
  </div>

  <PasswordDialog v-if="passwordFor" :user="passwordFor" @close="passwordFor = null" @done="passwordDone" />
</template>
