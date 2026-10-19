<script setup>
// Input with themed suggestions (replaces the native <datalist>, which the
// browser draws with rounded corners). Filters by a substring of the value or
// the label; arrow keys, Enter and Esc. The list is position: fixed, computed
// from the input, so the scrolling modal body does not clip it.
// Events: update:modelValue; pick(value) on click/Enter on a suggestion;
// enter when Enter is pressed with no suggestion highlighted.
import { computed, nextTick, onBeforeUnmount, ref } from 'vue'

defineOptions({ inheritAttrs: false })
const props = defineProps({
  modelValue: { type: String, default: '' },
  options: { type: Array, default: () => [] }, // strings or { value, label }
  max: { type: Number, default: 50 },
})
const emit = defineEmits(['update:modelValue', 'pick', 'enter'])

const field = ref(null)
const open = ref(false)
const active = ref(-1)
const pos = ref({})
const uid = Math.random().toString(36).slice(2, 8)

const items = computed(() => {
  const q = props.modelValue.trim().toLowerCase()
  return props.options
    .map((o) => (typeof o === 'string' ? { value: o, label: '' } : o))
    .filter((o) => !q || o.value.toLowerCase().includes(q) || o.label?.toLowerCase().includes(q))
    .slice(0, props.max)
})

// place puts the list below the input, or above it when there is no room.
// QHD/4K apply CSS zoom to the page (style.css): the input's rect is in viewport pixels but left/top of a fixed
// element are zoomed, so everything is divided by the zoom (as the tooltip in native.js does).
function place() {
  const z = parseFloat(getComputedStyle(document.documentElement).zoom) || 1
  const b = field.value?.getBoundingClientRect()
  if (!b) return
  const r = { left: b.left / z, top: b.top / z, bottom: b.bottom / z, width: b.width / z }
  const h = window.innerHeight / z
  const below = h - r.bottom
  pos.value =
    below < 180 && r.top > below
      ? { left: r.left + 'px', width: r.width + 'px', bottom: h - r.top + 2 + 'px', maxHeight: r.top - 12 + 'px' }
      : { left: r.left + 'px', width: r.width + 'px', top: r.bottom + 2 + 'px', maxHeight: Math.min(260, below - 12) + 'px' }
}
function openList() {
  open.value = true
  active.value = -1
  place()
  window.addEventListener('scroll', place, true)
  window.addEventListener('resize', place)
}
function closeList() {
  open.value = false
  window.removeEventListener('scroll', place, true)
  window.removeEventListener('resize', place)
}
onBeforeUnmount(closeList)

function onInput(ev) {
  emit('update:modelValue', ev.target.value)
  if (!open.value) openList()
  active.value = -1
}
function pick(o) {
  emit('update:modelValue', o.value)
  emit('pick', o.value)
  closeList()
}
async function onKey(ev) {
  if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
    ev.preventDefault()
    if (!open.value) openList()
    const n = items.value.length
    if (!n) return
    active.value = ev.key === 'ArrowDown' ? (active.value + 1) % n : (active.value - 1 + n) % n
    await nextTick()
    document.getElementById(`sugg-${uid}-${active.value}`)?.scrollIntoView({ block: 'nearest' })
  } else if (ev.key === 'Enter') {
    if (open.value && active.value >= 0 && items.value[active.value]) {
      ev.preventDefault() // pick only: do not submit the form
      pick(items.value[active.value])
    } else {
      closeList()
      emit('enter', ev)
    }
  } else if (ev.key === 'Escape' && open.value) {
    ev.stopPropagation() // close the list, not the modal
    ev.preventDefault()
    closeList()
  }
}
</script>

<template>
  <div class="relative w-full">
    <input
      ref="field"
      v-bind="$attrs"
      :value="modelValue"
      class="inp"
      autocomplete="off"
      role="combobox"
      :aria-expanded="open && items.length > 0"
      :aria-controls="`sugg-${uid}`"
      :aria-activedescendant="active >= 0 ? `sugg-${uid}-${active}` : undefined"
      @input="onInput"
      @focus="openList"
      @click="open || openList()"
      @blur="closeList"
      @keydown="onKey"
    />
    <ul v-if="open && items.length" :id="`sugg-${uid}`" role="listbox" class="suggestions" :style="pos">
      <li
        v-for="(o, i) in items"
        :id="`sugg-${uid}-${i}`"
        :key="o.value"
        role="option"
        :aria-selected="i === active"
        :class="{ active: i === active }"
        @mousedown.prevent="pick(o)"
        @mousemove="active = i"
      >
        <span class="truncate">{{ o.value }}</span>
        <span v-if="o.label" class="hint truncate">{{ o.label }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.suggestions {
  position: fixed;
  z-index: 60;
  overflow-y: auto;
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 12px;
  background: var(--color-field);
  border: 1px solid var(--color-pop-line);
  border-top: 3px solid var(--color-accent);
  box-shadow: var(--shadow-pop);
}
.suggestions li {
  display: flex;
  gap: 12px;
  align-items: baseline;
  justify-content: space-between;
  padding: 5px 8px;
  color: var(--color-on-pop);
  cursor: pointer;
  border-bottom: 1px solid var(--color-line-soft);
}
.suggestions li:nth-child(even) {
  background: var(--color-row-hover);
}
.suggestions li.active {
  color: var(--color-on-selection);
  background: var(--color-selection);
}
.hint {
  font-size: 11px;
  color: var(--color-fg-muted);
}
.active .hint {
  color: var(--color-on-selection-muted);
}
</style>
