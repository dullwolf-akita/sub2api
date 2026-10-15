<template>
  <div class="min-h-screen bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-gray-100">
    <header class="border-b border-gray-200 bg-white px-4 py-4 dark:border-dark-700 dark:bg-dark-900 sm:px-6">
      <div class="mx-auto flex max-w-[1400px] flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-lg font-bold">费用查询导出（分享）</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            <template v-if="meta">{{ meta.email }} · {{ meta.start_date }} ~ {{ meta.end_date }} · 链接有效至 {{ formatExpiry(meta.expires_at) }}</template>
            <template v-else-if="loading">正在验证分享链接…</template>
            <template v-else-if="fatalError">{{ fatalError }}</template>
          </p>
        </div>
        <button
          v-if="meta && !fatalError"
          type="button"
          class="rounded-lg bg-primary-600 px-4 py-2 text-sm font-semibold text-white hover:bg-primary-700 disabled:opacity-50"
          :disabled="exporting"
          @click="onExport"
        >
          {{ exporting ? '准备下载…' : '导出 Excel/CSV（流式）' }}
        </button>
      </div>
    </header>

    <main v-if="meta && stats && !fatalError" class="mx-auto max-w-[1400px] space-y-4 px-4 py-4 sm:px-6">
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
        <div v-for="c in statCards" :key="c.label" class="rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900">
          <div class="text-xs text-gray-500">{{ c.label }}</div>
          <div class="mt-1 text-lg font-bold tabular-nums">{{ c.value }}</div>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 lg:grid-cols-[340px_1fr]" style="min-height: 480px">
        <aside class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
          <div class="border-b border-gray-200 px-3 py-2 text-sm font-semibold text-gray-600 dark:border-dark-700">
            按请求模型消费（点击过滤）
          </div>
          <div class="min-h-0 flex-1 overflow-auto p-2">
            <table class="w-full text-left text-xs">
              <thead class="sticky top-0 bg-white dark:bg-dark-900">
                <tr class="text-gray-500">
                  <th class="px-2 py-1">模型</th>
                  <th class="px-2 py-1 text-right">次数</th>
                  <th class="px-2 py-1 text-right">用户扣费</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="m in stats.models"
                  :key="m.model"
                  class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-800"
                  :class="{ 'bg-primary-50 dark:bg-primary-900/20': filterModel === m.model }"
                  @click="toggleModel(m.model)"
                >
                  <td class="max-w-[140px] truncate px-2 py-1.5" :title="m.model">{{ m.model }}</td>
                  <td class="px-2 py-1.5 text-right tabular-nums">{{ m.count.toLocaleString() }}</td>
                  <td class="px-2 py-1.5 text-right tabular-nums">{{ fmtMoney(m.actual_cost) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </aside>

        <section class="flex min-h-0 flex-col overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900">
          <div class="flex flex-wrap items-center gap-2 border-b border-gray-200 px-3 py-2 dark:border-dark-700">
            <input
              v-model="searchQ"
              type="search"
              class="min-w-[200px] flex-1 rounded-lg border border-gray-300 bg-white px-3 py-1.5 text-sm dark:border-dark-600 dark:bg-dark-800"
              placeholder="搜索模型 / 密钥 / 账户 / 请求ID / IP…"
              @keyup.enter="reloadRows"
            />
            <select v-model.number="pageSize" class="rounded-lg border border-gray-300 px-2 py-1.5 text-sm dark:border-dark-600 dark:bg-dark-800" @change="page = 1; reloadRows()">
              <option :value="50">50</option>
              <option :value="100">100</option>
              <option :value="200">200</option>
              <option :value="500">500</option>
            </select>
            <button type="button" class="rounded-lg border px-3 py-1.5 text-sm disabled:opacity-40" :disabled="page <= 1 || rowsLoading" @click="page--; reloadRows()">上一页</button>
            <span class="text-xs text-gray-500 tabular-nums">{{ pageInfo }}</span>
            <button type="button" class="rounded-lg border px-3 py-1.5 text-sm disabled:opacity-40" :disabled="!hasNext || rowsLoading" @click="page++; reloadRows()">下一页</button>
          </div>
          <div class="min-h-0 flex-1 overflow-auto">
            <div v-if="rowsLoading" class="p-8 text-center text-sm text-gray-500">加载明细…</div>
            <table v-else class="min-w-full whitespace-nowrap text-xs">
              <thead class="sticky top-0 z-[1] bg-gray-50 dark:bg-dark-800">
                <tr>
                  <th v-for="col in displayCols" :key="col" class="border-b px-2 py-2 text-left font-semibold text-gray-500 dark:border-dark-700">
                    {{ labels[col] || col }}
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="(row, idx) in rows"
                  :key="String(row.id ?? idx)"
                  class="border-b border-gray-100 odd:bg-white even:bg-gray-50/60 dark:border-dark-700 dark:odd:bg-dark-900 dark:even:bg-dark-800/40"
                >
                  <td v-for="col in displayCols" :key="col" class="max-w-[240px] truncate px-2 py-1.5" :title="formatCell(col, row[col])">
                    {{ formatCell(col, row[col]) }}
                  </td>
                </tr>
                <tr v-if="!rows.length">
                  <td :colspan="displayCols.length" class="p-8 text-center text-gray-500">无数据</td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </div>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  getShareMeta,
  getShareStats,
  getShareRows,
  shareExportUrl,
  type ShareMeta
} from '@/api/shareCostQueryRealtime'
import type { CostQueryStats } from '@/api/admin/costQueryRealtime'

const route = useRoute()
const hashcode = computed(() => String(route.query.hashcode || '').trim())

const loading = ref(true)
const fatalError = ref('')
const meta = ref<ShareMeta | null>(null)
const stats = ref<CostQueryStats | null>(null)
const rows = ref<Record<string, unknown>[]>([])
const labels = ref<Record<string, string>>({})
const apiColumns = ref<string[]>([])
const rowsTotal = ref(0)
const rowsLoading = ref(false)
const filterModel = ref('')
const searchQ = ref('')
const page = ref(1)
const pageSize = ref(100)
const exporting = ref(false)

const preferredCols = [
  'created_at', 'user_email', 'api_key_name', 'requested_model', 'upstream_model',
  'upstream_response_model', 'upstream_model_mismatch', 'requested_reasoning_effort',
  'reasoning_effort', 'group_name', 'inbound_endpoint', 'upstream_endpoint',
  'request_type_label', 'input_tokens', 'output_tokens', 'cache_read_tokens',
  'cache_creation_tokens', 'input_cost', 'output_cost', 'cache_read_cost',
  'cache_creation_cost', 'rate_multiplier', 'account_rate_multiplier', 'total_cost',
  'actual_cost', 'account_billed', 'first_token_ms', 'duration_ms', 'request_id',
  'upstream_request_id', 'user_agent', 'ip_address'
]
const displayCols = computed(() => (apiColumns.value.length ? apiColumns.value : preferredCols))

const pageInfo = computed(() => {
  const total = rowsTotal.value
  if (!total) return '0 / 0'
  const from = (page.value - 1) * pageSize.value + 1
  const to = Math.min(page.value * pageSize.value, total)
  return `${from}-${to} / ${total.toLocaleString()}`
})
const hasNext = computed(() => page.value * pageSize.value < rowsTotal.value)

const statCards = computed(() => {
  const s = stats.value
  if (!s) return []
  return [
    { label: '总调用', value: s.total_calls.toLocaleString() },
    { label: '用户扣费', value: fmtMoney(s.total_actual_cost) },
    { label: '原始费用', value: fmtMoney(s.total_cost) },
    { label: '输入 Token', value: Math.round(s.total_input_tokens).toLocaleString() },
    { label: '输出 Token', value: Math.round(s.total_output_tokens).toLocaleString() },
    { label: '模型数', value: String(s.model_count) }
  ]
})

function fmtMoney(n: number) {
  return Number(n || 0).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 6 })
}

function formatZhTime(v: unknown) {
  if (v == null || v === '') return ''
  const d = new Date(String(v))
  if (Number.isNaN(d.getTime())) return String(v)
  const parts = new Intl.DateTimeFormat('zh-CN', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
    hour12: false
  }).formatToParts(d)
  const g = (t: string) => parts.find(x => x.type === t)?.value || ''
  return `${g('year')}年${g('month')}月${g('day')}日 ${g('hour')}:${g('minute')}:${g('second')}`
}

function formatCell(col: string, v: unknown) {
  if (v == null || v === '') return ''
  if (col === 'created_at') return formatZhTime(v)
  if (typeof v === 'boolean') return v ? '是' : '否'
  if (typeof v === 'number' && (col.includes('cost') || col.includes('multiplier'))) return fmtMoney(v)
  return String(v)
}

function formatExpiry(iso: string) {
  return formatZhTime(iso)
}

function setNoReferrer() {
  let metaEl = document.querySelector('meta[name="referrer"]') as HTMLMetaElement | null
  if (!metaEl) {
    metaEl = document.createElement('meta')
    metaEl.name = 'referrer'
    document.head.appendChild(metaEl)
  }
  metaEl.content = 'no-referrer'
}

async function bootstrap() {
  setNoReferrer()
  if (!hashcode.value) {
    fatalError.value = '链接无效'
    loading.value = false
    return
  }
  try {
    meta.value = await getShareMeta(hashcode.value)
    labels.value = meta.value.labels || {}
    if (meta.value.columns?.length) apiColumns.value = meta.value.columns
    stats.value = await getShareStats(hashcode.value)
    await reloadRows()
  } catch (e: any) {
    const status = e?.status
    if (status === 410) fatalError.value = '分享链接已过期'
    else fatalError.value = '分享链接无效或已失效'
    meta.value = null
    stats.value = null
  } finally {
    loading.value = false
  }
}

async function reloadRows() {
  if (!hashcode.value) return
  rowsLoading.value = true
  try {
    const res = await getShareRows(hashcode.value, {
      offset: (page.value - 1) * pageSize.value,
      limit: pageSize.value,
      model: filterModel.value || undefined,
      q: searchQ.value.trim() || undefined
    })
    rows.value = res.rows
    rowsTotal.value = res.total
    labels.value = res.labels || labels.value
    if (res.columns?.length) apiColumns.value = res.columns
  } catch {
    rows.value = []
  } finally {
    rowsLoading.value = false
  }
}

function toggleModel(model: string) {
  filterModel.value = filterModel.value === model ? '' : model
  page.value = 1
  reloadRows()
}

function onExport() {
  if (!hashcode.value) return
  exporting.value = true
  try {
    const href = shareExportUrl(hashcode.value, {
      model: filterModel.value || undefined,
      q: searchQ.value.trim() || undefined
    })
    const link = document.createElement('a')
    link.href = href
    link.rel = 'noopener noreferrer'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  } finally {
    setTimeout(() => { exporting.value = false }, 800)
  }
}

onMounted(bootstrap)
</script>
