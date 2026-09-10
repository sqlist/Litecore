// 与 gateway（:8080）的 HTTP 接口约定。字段命名与 gateway 的 JSON 一一对应。

export type Scenario = 'stable' | 'edge' | 'degrading' | 'mixed';

export const SCENARIOS: Record<Scenario, { label: string; description: string }> = {
  stable: { label: '稳定信道', description: '信号充足，适合观察完整注册流程。' },
  edge: { label: '小区边缘', description: '接近接入门限，多数样本仍满足本次注册条件。' },
  degrading: { label: '弱信号', description: '低于接入门限，观察 AMF 拒绝注册。' },
  mixed: { label: '混合信道', description: '70% 稳定、20% 边缘、10% 恶化，每次随机。' },
};

export type RegisterResult = {
  success: boolean;
  ueId: string;
  amfUeId?: string;
  sessionId?: string;
  ueIp?: string;
  signalPower: number;
  sinr: number;
  retries: number;
  processingMs: number;
  gatewayMs: number;
  message: string;
};

export type UEEntry = {
  ueId: string;
  amfUeId?: string;
  sessionId?: string;
  ueIp?: string;
  amfState?: string;
  sessionState?: string;
  ruleId?: string;
  packetsForwarded: number;
  active: boolean;
  registeredAt?: string;
};

export type ServiceStatus = 'SERVING' | 'NOT_SERVING' | 'UNREACHABLE';
export type ServiceHealth = { name: string; address: string; status: ServiceStatus };
export type Health = { gateway: string; checkedAt: string; services: ServiceHealth[] };

export type DeregisterResult = { success: boolean; ueId: string; message: string; releasedIp?: string };

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? 'http://localhost:8080';

export class ApiError extends Error {}

async function request<T>(path: string, init?: RequestInit, timeoutMs = 6000): Promise<T> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), timeoutMs);
  let data: T & { error?: string };
  try {
    const response = await fetch(`${API_BASE}${path}`, { ...init, signal: controller.signal });
    data = (await response.json()) as T & { error?: string };
  } catch (cause) {
    const aborted = cause instanceof DOMException && cause.name === 'AbortError';
    throw new ApiError(aborted ? '请求超时，网关没有回应' : '连不上网关（:8080）');
  } finally {
    window.clearTimeout(timer);
  }
  if (data && typeof data === 'object' && typeof data.error === 'string') {
    throw new ApiError(data.error);
  }
  return data;
}

export function apiHealth(): Promise<Health> {
  return request<Health>('/api/health', undefined, 2500);
}

// 注册在网关内部可能带重试（最多 2 次，每次最长 4 秒），给足超时。
export function apiRegister(ueId: string, scenario: Scenario): Promise<RegisterResult> {
  return request<RegisterResult>(
    '/api/register',
    { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ueId, scenario }) },
    20000,
  );
}

export function apiDeregister(ueId: string): Promise<DeregisterResult> {
  return request<DeregisterResult>(
    '/api/deregister',
    { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ ueId }) },
    10000,
  );
}

export function apiUEs(): Promise<{ ues: UEEntry[] }> {
  return request<{ ues: UEEntry[] }>('/api/ues', undefined, 10000);
}
