<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-xl font-bold text-gray-900 dark:text-white">费用实时查询</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            按用户邮箱 + 日期区间异步拉取全量 usage，在线分页查看，流式导出 CSV（中文表头）
          </p>
        </div>
        <button
          v-if="jobId && jobStatus === 'done'"
          type="button"
          class="btn btn-primary"
          :disabled="exporting"
          @click="onExport"
        >
          {{ exporting ? '准备下载…' : '导出 Excel/CSV（流式）' }}
        </button>
      </div>

      <!-- Query form -->
      <div class="card p-4">
        <div class="flex flex-wrap items-end gap-4">
          <div class="min-w-[240px] flex-1">
            <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">用户邮箱</label>
            <input
              v-model="email"
              type="email"
              list="cqr-email-suggestions"
              class="input w-full"
              placeholder="user@example.com"
              :disabled="busy"
              @input="onEmailInput"
            />
            <datalist id="cqr-email-suggestions">
              <option v-for="u in emailSuggestions" :key="u.id" :value="u.email" />
            </datalist>
          </div>
          <div>
            <label class="mb-1 block text-sm font-medium text-gray-700 dark:text-gray-300">起止日期（上海时区，半开区间）</label>
            <DateRangePicker
              v-model:start-date="startDate"
              v-model:end-date="endDate"
            />
          </div>
          <button type="button" class="btn btn-primary" :disabled="busy || !canQuery" @click="onQuery">
            {{ busy ? '查询中…' : '查询' }}
          </button>
        </div>
        <p v-if="formError" class="mt-2 text-sm text-red-500">{{ formError }}</p>
      </div>

      <!-- Progress -->
      <div v-if="jobId && jobStatus !== 'done'" class="card p-4">
        <div class="mb-2 flex items-center justify-between text-sm">
          <span class="text-gray-600 dark:text-gray-300">
            任务 {{ jobId.slice(0, 8) }} · 状态 {{ statusLabel }}
          </span>
          <span class="tabular-nums text-gray-500">
            已处理 {{ rowsDone.toLocaleString() }}
            <template v-if="totalRows != null"> / {{ totalRows.toLocaleString() }}</template>
            行 · {{ progress.toFixed(1) }}%
          </span>
        </div>
        <div class="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
          <div
            class="h-full rounded-full bg-primary-500 transition-all"
            :style="{ width: Math.min(100, Math.max(progress, jobStatus === 'pending' ? 2 : 0)) + '%' }"
          />
        </div>
        <p v-if="jobError" class="mt-2 text-sm text-red-500">{{ jobError }}</p>
      </div>

      <!-- Results -->
      <template v-if="jobStatus === 'done' && stats">
        <div class="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
          <div v-for="c in statCards" :key="c.label" class="card p-3">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ c.label }}</div>
            <div class="mt-1 text-lg font-bold tabular-nums text-gray-900 dark:text-white">{{ c.value }}</div>
          </div>
        </div>

        <div class="grid grid-cols-1 gap-4 lg:grid-cols-[340px_1fr]" style="min-height: 480px">
          <aside class="card flex min-h-0 flex-col overflow-hidden">
            <div class="border-b border-gray-200 px-3 py-2 text-sm font-semibold text-gray-600 dark:border-dark-700 dark:text-gray-300">
              按请求模型消费（点击过滤）
            </div>
            <div class="min-h-0 flex-1 overflow-auto p-2">
              <table class="w-full text-left text-xs">
                <thead class="sticky top-0 bg-white dark:bg-dark-800">
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
                    class="cursor-pointer border-t border-gray-100 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-700/50"
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

          <section class="card flex min-h-0 flex-col overflow-hidden">
            <div class="flex flex-wrap items-center gap-2 border-b border-gray-200 px-3 py-2 dark:border-dark-700">
              <input
                v-model="searchQ"
                type="search"
                class="input min-w-[200px] flex-1"
                placeholder="搜索模型 / 密钥 / 账户 / 请求ID / IP…"
                @keyup.enter="reloadRows"
              />
              <select v-model.number="pageSize" class="input w-24" @change="page = 1; reloadRows()">
                <option :value="50">50</option>
                <option :value="100">100</option>
                <option :value="200">200</option>
                <option :value="500">500</option>
              </select>
              <button type="button" class="btn btn-secondary" :disabled="page <= 1 || rowsLoading" @click="page--; reloadRows()">上一页</button>
              <span class="text-xs text-gray-500 tabular-nums">{{ pageInfo }}</span>
              <button type="button" class="btn btn-secondary" :disabled="!hasNext || rowsLoading" @click="page++; reloadRows()">下一页</button>
              <span
                v-if="filterModel"
                class="inline-flex items-center gap-1 rounded-full bg-primary-50 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              >
                模型: {{ filterModel }}
                <button type="button" class="opacity-70 hover:opacity-100" @click="filterModel = ''; reloadRows()">×</button>
              </span>
            </div>
            <div class="min-h-0 flex-1 overflow-auto">
              <div v-if="rowsLoading" class="p-8 text-center text-sm text-gray-500">加载明细…</div>
              <table v-else class="min-w-full whitespace-nowrap text-xs">
                <thead class="sticky top-0 z-[1] bg-gray-50 dark:bg-dark-800">
                  <tr>
                    <th
                      v-for="col in preferredCols"
                      :key="col"
                      class="cursor-pointer border-b border-gray-200 px-2 py-2 text-left font-semibold text-gray-500 dark:border-dark-700"
                      @click="toggleSort(col)"
                    >
                      {{ labels[col] || col }}
                      <span v-if="sort === col">{{ order === 'asc' ? '↑' : '↓' }}</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="(row, idx) in rows"
                    :key="String(row.id ?? idx)"
                    class="border-b border-gray-100 odd:bg-white even:bg-gray-50/60 hover:bg-primary-50/40 dark:border-dark-700 dark:odd:bg-dark-900 dark:even:bg-dark-800/40"
                  >
                    <td v-for="col in preferredCols" :key="col" class="max-w-[240px] truncate px-2 py-1.5" :title="cellTitle(row, col)">
                      {{ formatCell(col, row[col]) }}
                    </td>
                  </tr>
                  <tr v-if="!rows.length">
                    <td :colspan="preferredCols.length" class="p-8 text-center text-gray-500">无数据</td>
                  </tr>
                </tbody>
              </table>
            </div>
            <div class="border-t border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700">
              {{ stats.email }} · {{ stats.start_date }} ~ {{ stats.end_date }} · 费用优先「用户扣费 actual_cost」
            </div>
          </section>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import { searchUsers } from '@/api/admin/usage'
import {
  createJob,
  getJob,
  getStats,
  getRows,
  downloadExportStreaming,
  type CostQueryJobStatus,
  type CostQueryStats
} from '@/api/admin/costQueryRealtime'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const email = ref('')
const startDate = ref('')
const endDate = ref('')
const emailSuggestions = ref<{ id: number; email: string }[]>([])
let emailTimer: ReturnType<typeof setTimeout> | null = null

const formError = ref('')
const jobId = ref('')
const jobStatus = ref<CostQueryJobStatus | ''>('')
const progress = ref(0)
const rowsDone = ref(0)
const totalRows = ref<number | null>(null)
const jobError = ref('')
const busy = ref(false)
let pollTimer: ReturnType<typeof setInterval> | null = null

const stats = ref<CostQueryStats | null>(null)
const rows = ref<Record<string, unknown>[]>([])
const labels = ref<Record<string, string>>({})
const rowsTotal = ref(0)
const rowsLoading = ref(false)
const filterModel = ref('')
const searchQ = ref('')
const page = ref(1)
const pageSize = ref(100)
const sort = ref('created_at')
const order = ref<'asc' | 'desc'>('desc')
const exporting = ref(false)

const preferredCols = [
  'created_at', 'requested_model', 'api_key_name', 'account_name', 'group_name',
  'request_type_label', 'billing_type_label', 'input_tokens', 'output_tokens',
  'actual_cost', 'total_cost', 'rate_multiplier', 'duration_ms', 'ip_address', 'request_id'
]

const canQuery = computed(() => !!email.value.trim() && !!startDate.value && !!endDate.value)
const statusLabel = computed(() => {
  switch (jobStatus.value) {
    case 'pending': return '排队中'
    case 'running': return '拉取中'
    case 'failed': return '失败'
    case 'done': return '完成'
    default: return '—'
  }
})
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
  if (typeof v === 'number' && (col.includes('cost') || col.includes('multiplier'))) {
    return fmtMoney(v)
  }
  return String(v)
}

function cellTitle(row: Record<string, unknown>, col: string) {
  return formatCell(col, row[col])
}

function onEmailInput() {
  if (emailTimer) clearTimeout(emailTimer)
  emailTimer = setTimeout(async () => {
    const q = email.value.trim()
    if (q.length < 2) {
      emailSuggestions.value = []
      return
    }
    try {
      emailSuggestions.value = await searchUsers(q)
    } catch {
      emailSuggestions.value = []
    }
  }, 250)
}

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function onQuery() {
  formError.value = ''
  jobError.value = ''
  stats.value = null
  rows.value = []
  filterModel.value = ''
  searchQ.value = ''
  page.value = 1
  if (!canQuery.value) {
    formError.value = '请填写邮箱和日期'
    return
  }
  busy.value = true
  stopPoll()
  try {
    const tz = Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'
    const res = await createJob({
      email: email.value.trim(),
      start: startDate.value,
      end: endDate.value,
      timezone: tz
    })
    jobId.value = res.job_id
    jobStatus.value = 'pending'
    progress.value = 0
    rowsDone.value = 0
    totalRows.value = null
    pollTimer = setInterval(pollJob, 1000)
    await pollJob()
  } catch (e: any) {
    formError.value = e?.message || '创建任务失败'
    busy.value = false
  }
}

async function pollJob() {
  if (!jobId.value) return
  try {
    const j = await getJob(jobId.value)
    jobStatus.value = j.status
    progress.value = j.progress || 0
    rowsDone.value = j.rows_done || 0
    totalRows.value = j.total_rows
    jobError.value = j.error || ''
    if (j.status === 'done') {
      stopPoll()
      busy.value = false
      await loadStatsAndRows()
    } else if (j.status === 'failed') {
      stopPoll()
      busy.value = false
      appStore.showError(j.error || '查询失败')
    }
  } catch (e: any) {
    stopPoll()
    busy.value = false
    formError.value = e?.message || '轮询失败'
  }
}

async function loadStatsAndRows() {
  if (!jobId.value) return
  stats.value = await getStats(jobId.value)
  await reloadRows()
}

async function reloadRows() {
  if (!jobId.value || jobStatus.value !== 'done') return
  rowsLoading.value = true
  try {
    const res = await getRows(jobId.value, {
      offset: (page.value - 1) * pageSize.value,
      limit: pageSize.value,
      model: filterModel.value || undefined,
      q: searchQ.value.trim() || undefined,
      sort: sort.value,
      order: order.value
    })
    rows.value = res.rows
    rowsTotal.value = res.total
    labels.value = res.labels || {}
  } catch (e: any) {
    appStore.showError(e?.message || '加载明细失败')
  } finally {
    rowsLoading.value = false
  }
}

function toggleModel(model: string) {
  filterModel.value = filterModel.value === model ? '' : model
  page.value = 1
  reloadRows()
}

function toggleSort(col: string) {
  if (sort.value === col) {
    order.value = order.value === 'asc' ? 'desc' : 'asc'
  } else {
    sort.value = col
    order.value = col === 'created_at' ? 'desc' : 'asc'
  }
  reloadRows()
}

async function onExport() {
  if (!jobId.value) return
  exporting.value = true
  try {
    await downloadExportStreaming(jobId.value, {
      model: filterModel.value || undefined,
      q: searchQ.value.trim() || undefined
    })
  } catch (e: any) {
    appStore.showError(e?.message || '导出失败')
  } finally {
    exporting.value = false
  }
}

watch(searchQ, () => {
  // debounce search via enter / model click; optional soft debounce
})

onBeforeUnmount(() => {
  stopPoll()
  if (emailTimer) clearTimeout(emailTimer)
})

// default date: this month
;(() => {
  const now = new Date()
  const y = now.getFullYear()
  const m = String(now.getMonth() + 1).padStart(2, '0')
  const d = String(now.getDate()).padStart(2, '0')
  startDate.value = `${y}-${m}-01`
  endDate.value = `${y}-${m}-${d}`
})()
</script>
