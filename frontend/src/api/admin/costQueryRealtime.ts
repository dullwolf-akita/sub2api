/**
 * Admin Cost Query Realtime API (localext)
 * Async job + paginated online table + streaming CSV export.
 */

import { apiClient } from '../client'

export type CostQueryJobStatus = 'pending' | 'running' | 'done' | 'failed'

export interface CostQueryJob {
  job_id: string
  status: CostQueryJobStatus
  email: string
  user_id: number
  start_date: string
  end_date: string
  timezone: string
  progress: number
  rows_done: number
  total_rows: number | null
  error?: string
  created_by: number
  created_at: string
  updated_at: string
  finished_at?: string
}

export interface CostQueryModelStat {
  model: string
  count: number
  actual_cost: number
  total_cost: number
  input_tokens: number
  output_tokens: number
}

export interface CostQueryStats {
  total_calls: number
  total_actual_cost: number
  total_cost: number
  total_input_tokens: number
  total_output_tokens: number
  model_count: number
  models: CostQueryModelStat[]
  email: string
  start_date: string
  end_date: string
}

export interface CostQueryRowsResponse {
  total: number
  offset: number
  limit: number
  columns: string[]
  labels: Record<string, string>
  rows: Record<string, unknown>[]
}

export async function createJob(payload: {
  email: string
  start: string
  end: string
  timezone?: string
}): Promise<{ job_id: string }> {
  const { data } = await apiClient.post<{ job_id: string }>(
    '/admin/cost-query-realtime/jobs',
    payload
  )
  return data
}

export async function getJob(jobId: string): Promise<CostQueryJob> {
  const { data } = await apiClient.get<CostQueryJob>(
    `/admin/cost-query-realtime/jobs/${jobId}`
  )
  return data
}

export async function getStats(jobId: string): Promise<CostQueryStats> {
  const { data } = await apiClient.get<CostQueryStats>(
    `/admin/cost-query-realtime/jobs/${jobId}/stats`
  )
  return data
}

export async function getRows(
  jobId: string,
  params: { offset?: number; limit?: number; model?: string; q?: string; sort?: string; order?: string }
): Promise<CostQueryRowsResponse> {
  const { data } = await apiClient.get<CostQueryRowsResponse>(
    `/admin/cost-query-realtime/jobs/${jobId}/rows`,
    { params }
  )
  return data
}

/** Create one-time ticket then trigger native browser download (streaming, no full buffer). */
export async function downloadExportStreaming(
  jobId: string,
  params: { model?: string; q?: string } = {}
): Promise<void> {
  const { data } = await apiClient.post<{ ticket: string; url: string }>(
    `/admin/cost-query-realtime/jobs/${jobId}/export-ticket`,
    {},
    { params }
  )
  const href = data.url.startsWith('http')
    ? data.url
    : `${window.location.origin}${data.url.startsWith('/') ? '' : '/'}${data.url}`
  const link = document.createElement('a')
  link.href = href
  link.rel = 'noopener'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

export const costQueryRealtimeAPI = {
  createJob,
  getJob,
  getStats,
  getRows,
  downloadExportStreaming
}

export default costQueryRealtimeAPI
