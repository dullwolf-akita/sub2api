/**
 * Public share APIs for cost-query-realtime (no admin auth; hashcode required).
 */
import { apiClient } from './client'
import type { CostQueryRowsResponse, CostQueryStats } from './admin/costQueryRealtime'

export interface ShareMeta {
  email: string
  start_date: string
  end_date: string
  timezone: string
  expires_at: string
  columns: string[]
  labels: Record<string, string>
}

export async function getShareMeta(hashcode: string): Promise<ShareMeta> {
  const { data } = await apiClient.get<ShareMeta>('/share/cost-query-realtime/meta', {
    params: { hashcode }
  })
  return data
}

export async function getShareStats(hashcode: string): Promise<CostQueryStats> {
  const { data } = await apiClient.get<CostQueryStats>('/share/cost-query-realtime/stats', {
    params: { hashcode }
  })
  return data
}

export async function getShareRows(
  hashcode: string,
  params: { offset?: number; limit?: number; model?: string; q?: string; sort?: string; order?: string }
): Promise<CostQueryRowsResponse> {
  const { data } = await apiClient.get<CostQueryRowsResponse>('/share/cost-query-realtime/rows', {
    params: { ...params, hashcode }
  })
  return data
}

export function shareExportUrl(hashcode: string, params: { model?: string; q?: string } = {}): string {
  const q = new URLSearchParams({ hashcode })
  if (params.model) q.set('model', params.model)
  if (params.q) q.set('q', params.q)
  return `/api/v1/share/cost-query-realtime/export.csv?${q.toString()}`
}
