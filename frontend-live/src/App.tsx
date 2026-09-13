import { Fragment, useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import {
  apiDeregister,
  apiHealth,
  apiRegister,
  apiUEs,
  SCENARIOS,
  type Health,
  type RegisterResult,
  type Scenario,
  type UEEntry,
} from './api';
import { demoRegister, demoUEs } from './demo';
import { GateChecklist } from './components/GateChecklist';
import { HealthHistoryStrip, type HealthSnapshot } from './components/HealthHistoryStrip';
import { pct } from './components/LatencyHistogram';
import type { LoadLatencyPoint } from './components/LatencyEvolution';
import { LoadTestPanel, type LoadRunState, type LoadRunSummary } from './components/LoadTestPanel';
import { PacketCurve, type PacketPoint } from './components/PacketCurve';
import { PacketSparkline } from './components/PacketSparkline';
import { RetryTimeline } from './components/RetryTimeline';
import { SignalScatter, type ScatterSample } from './components/SignalScatter';
import { UEResourceDetail } from './components/UEResourceDetail';

type Mode = 'probing' | 'live' | 'demo';

const NODES = [
  { id: 'UE', name: '模拟终端', task: '发起注册', port: '浏览器' },
  { id: 'AMF', name: '接入管理', task: '执行接入门控', port: ':50051' },
  { id: 'SMF', name: '会话管理', task: '分配会话与 IP', port: ':50052' },
  { id: 'UPF', name: '用户面', task: '模拟转发记账', port: ':50053' },
] as const;

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function flowStatus(phase: number, running: boolean, result: RegisterResult | null): string {
  if (running) {
    if (phase >= 2) return 'SMF 在分 IP，UPF 开始记账……';
    if (phase === 1) return 'AMF 在看信道——两道门槛：≥ −110 dBm 且 SINR ≥ 0 dB';
    return '正在敲门：UE → AMF';
  }
  if (result) {
    if (result.success) return '注册完成，会话已建立';
    return result.retries > 0 ? '后端没接上，注册没有完成' : '请求在接入检查时停了下来';
  }
  return '等待发起一次注册';
}

function resultHeadline(result: RegisterResult): string {
  if (result.success) return '信号不错，接入完成。';
  if (result.retries > 0) return `重试了 ${result.retries} 次，后端还是没接上。`;
  if (result.message.includes('信道')) return '信号不够稳，AMF 没让进门。';
  return '这次没成，看看后端怎么说。';
}

function healthLine(mode: Mode, health: Health | null): string {
  if (mode === 'probing') return '正在敲门问四个服务在不在……';
  if (mode === 'demo') return '后端没连上，现在看的是排练数据。';
  if (!health) return '正在敲门问四个服务在不在……';
  const down = health.services.filter((service) => service.status !== 'SERVING');
  if (down.length === 0) return '四个服务都在岗位上。';
  const exiting = down.filter((service) => service.status === 'NOT_SERVING');
  const unreachable = down.filter((service) => service.status === 'UNREACHABLE');
  const parts: string[] = [];
  if (exiting.length > 0) parts.push(`${exiting.map((service) => service.name).join('、')} 正在优雅退出（5 秒窗口）`);
  if (unreachable.length > 0) parts.push(`${unreachable.map((service) => service.name).join('、')} 联系不上`);
  return `${parts.join('；')}。别慌——这正好可以拿来演示容错。`;
}

function HealthDots({ mode, health }: { mode: Mode; health: Health | null }) {
  const names = ['网关', 'AMF', 'SMF', 'UPF'];
  return (
    <div className="service-health" role="status" aria-live="polite" aria-label="服务运行状态">
      {names.map((name) => {
        let status: string = 'probing';
        if (mode === 'live') {
          status = name === '网关' ? 'SERVING' : (health?.services.find((service) => service.name === name)?.status ?? 'probing');
        }
        const cls =
          mode === 'demo'
            ? 'demo'
            : status === 'SERVING'
              ? 'ok'
              : status === 'NOT_SERVING'
                ? 'warn'
                : status === 'probing'
                  ? 'probing'
                  : 'bad';
        const label =
          mode === 'demo'
            ? '演示'
            : status === 'SERVING'
              ? '在线'
              : status === 'NOT_SERVING'
                ? '退出中'
                : status === 'probing'
                  ? '检测中'
                  : '异常';
        return (
          <span key={name} className={`service-chip ${cls}`} title={`${name}：${label}`}>
            <span className="dot" aria-hidden="true" />
            <span>{name}</span>
            <strong>{label}</strong>
          </span>
        );
      })}
    </div>
  );
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function signalMeter(value: number): number {
  return clamp(((value + 125) / 50) * 100, 0, 100);
}

function sinrMeter(value: number): number {
  return clamp(((value + 5) / 30) * 100, 0, 100);
}

type HistoryRecord = { key: number; r: RegisterResult };

export default function App() {
  const [mode, setMode] = useState<Mode>('probing');
  const [health, setHealth] = useState<Health | null>(null);
  const [ues, setUes] = useState<UEEntry[]>([]);
  const [demoUes, setDemoUes] = useState<UEEntry[]>(() => demoUEs());
  const [scenario, setScenario] = useState<Scenario>('stable');
  const [ueId, setUeId] = useState('UE-LIVE-01');
  const [running, setRunning] = useState(false);
  const [phase, setPhase] = useState(-1);
  const [result, setResult] = useState<RegisterResult | null>(null);
  const [records, setRecords] = useState<HistoryRecord[]>([]);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  // —— 可视化新增状态 ——
  const [expandedUE, setExpandedUE] = useState<string | null>(null);
  const [samples, setSamples] = useState<ScatterSample[]>([]);
  const [healthSnapshots, setHealthSnapshots] = useState<HealthSnapshot[]>([]);
  const [packetSeries, setPacketSeries] = useState<PacketPoint[]>([]);
  const [packetSpark, setPacketSpark] = useState<Record<string, number[]>>({});
  const [loadInputs, setLoadInputs] = useState({ concurrency: '10', total: '30' });
  const [loadState, setLoadState] = useState<LoadRunState | null>(null);
  const [loadRuns, setLoadRuns] = useState<LoadRunSummary[]>([]);
  const alive = useRef(true);
  const busy = useRef(false);
  const nextKey = useRef(0);
  // —— 可视化新增 refs ——
  const seenPackets = useRef(new Map<string, number>()); // ueId → 最近一次包数
  const misses = useRef(new Map<string, number>()); // ueId → 连续缺席轮数
  const departed = useRef(0); // 已流失终端的包数累计
  const loadSeq = useRef(0);
  const lastHealthAt = useRef(0);
  const lastPacketAt = useRef(0);
  const demoUesRef = useRef<UEEntry[]>(demoUEs());

  // 挂载后先敲一次网关：通了就实时模式，不通就落到演示模式。
  useEffect(() => {
    alive.current = true;
    let cancelled = false;
    void apiHealth()
      .then((h) => {
        if (!cancelled && alive.current) {
          setHealth(h);
          setMode('live');
        }
      })
      .catch(() => {
        if (!cancelled) setMode('demo');
      });
    return () => {
      alive.current = false;
      cancelled = true;
    };
  }, []);

  // 健康采样：每格 3 秒（去抖 1 秒防重复写入）。
  const recordHealth = useCallback((h: Health) => {
    const now = Date.now();
    if (now - lastHealthAt.current < 1000) return;
    lastHealthAt.current = now;
    const statuses: Record<string, string> = {};
    for (const service of h.services) statuses[service.name] = service.status;
    setHealthSnapshots((prev) => [...prev.slice(-119), { t: now, statuses }]);
  }, []);

  // 包计数采样：聚合总量只增不减——终端缺席 ≥2 轮才并入"已流失"累计，缺席 1 轮沿用旧值。
  const applyPacketSample = useCallback((list: UEEntry[]) => {
    const now = Date.now();
    if (now - lastPacketAt.current < 600) return;
    lastPacketAt.current = now;
    const current = new Map<string, number>();
    for (const u of list) current.set(u.ueId, u.packetsForwarded);
    for (const [id, packets] of seenPackets.current) {
      if (current.has(id)) continue;
      const miss = (misses.current.get(id) ?? 0) + 1;
      if (miss >= 2) {
        misses.current.delete(id);
        seenPackets.current.delete(id);
        departed.current += packets;
      } else {
        misses.current.set(id, miss);
      }
    }
    for (const id of current.keys()) {
      misses.current.delete(id);
      seenPackets.current.set(id, current.get(id) ?? 0);
    }
    let total = departed.current;
    for (const packets of seenPackets.current.values()) total += packets;
    setPacketSeries((prev) => [...prev.slice(-119), { t: now, total }]);
    setPacketSpark((prev) => {
      const next: Record<string, number[]> = {};
      for (const [id, packets] of seenPackets.current) {
        next[id] = [...(prev[id] ?? []).slice(-39), packets];
      }
      return next;
    });
  }, []);

  // 实时模式：每 1.5 秒拉一次在线终端，每 3 秒敲一次健康检查。
  useEffect(() => {
    if (mode !== 'live') return;
    const pollUEs = async () => {
      try {
        const { ues: list } = await apiUEs();
        if (!alive.current) return;
        setUes(list);
        applyPacketSample(list);
      } catch {
        /* 网络抖动时保留上一份数据 */
      }
    };
    const pollHealth = async () => {
      try {
        const h = await apiHealth();
        if (!alive.current) return;
        setHealth(h);
        recordHealth(h);
      } catch {
        if (alive.current) setMode('demo');
      }
    };
    void pollUEs();
    void pollHealth();
    const uesTimer = window.setInterval(() => void pollUEs(), 1500);
    const healthTimer = window.setInterval(() => void pollHealth(), 3000);
    return () => {
      window.clearInterval(uesTimer);
      window.clearInterval(healthTimer);
    };
  }, [mode, applyPacketSample, recordHealth]);

  // 演示模式：包数每秒 +10，让排练数据也"活着"；每 5 秒再敲一次网关，通了自动切回实时。
  useEffect(() => {
    if (mode !== 'demo') return;
    const packetTimer = window.setInterval(() => {
      const next = demoUesRef.current.map((u) => ({ ...u, packetsForwarded: u.packetsForwarded + 10 }));
      demoUesRef.current = next;
      setDemoUes(next);
      applyPacketSample(next);
    }, 1000);
    const retryTimer = window.setInterval(() => {
      void apiHealth()
        .then((h) => {
          if (alive.current) {
            setHealth(h);
            setMode('live');
          }
        })
        .catch(() => {
          /* 网关还没起来 */
        });
    }, 5000);
    return () => {
      window.clearInterval(packetTimer);
      window.clearInterval(retryTimer);
    };
  }, [mode, applyPacketSample]);

  // 顶部通知 8 秒后自动消失。
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 8000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  // 演示列表更新时同步 ref，供包采样定时器读取（避免在 setState updater 里做副作用）。
  const updateDemoUes = useCallback((updater: (prev: UEEntry[]) => UEEntry[]) => {
    setDemoUes((prev) => {
      const next = updater(prev);
      demoUesRef.current = next;
      return next;
    });
  }, []);

  const run = useCallback(
    async (id: string, chosen: Scenario) => {
      if (busy.current || mode === 'probing') return;
      busy.current = true;
      setError('');
      setNotice('');
      setResult(null);
      setRunning(true);
      setPhase(0);
      await delay(240);
      try {
        let res: RegisterResult;
        if (mode === 'live') {
          setPhase(1);
          res = await apiRegister(id, chosen); // 真实请求；期间动画停在 AMF 一步
          if (!alive.current) return;
          if (res.success) {
            setPhase(2);
            await delay(320);
            setPhase(3);
            await delay(320);
          } else {
            await delay(420);
          }
        } else {
          await delay(240);
          setPhase(1);
          await delay(280);
          res = demoRegister(id, chosen, nextKey.current + 1);
          if (res.success) {
            setPhase(2);
            await delay(260);
            setPhase(3);
            await delay(260);
          }
        }
        if (!alive.current) return;
        setResult(res);
        setRecords((prev) => [{ key: nextKey.current++, r: res }, ...prev].slice(0, 6));
        setSamples((prev) => {
          const sample: ScatterSample = {
            t: prev.length + 1,
            signal: res.signalPower,
            sinr: res.sinr,
            ok: res.success,
            scenario: chosen,
          };
          return [...prev.slice(-199), sample];
        });
        if (mode === 'live' && res.success) {
          try {
            const { ues: list } = await apiUEs();
            if (alive.current) setUes(list);
          } catch {
            /* 列表会由轮询兜底 */
          }
        }
        if (mode === 'demo' && res.success) {
          updateDemoUes((prev) => [
            {
              ueId: res.ueId,
              amfUeId: res.amfUeId,
              sessionId: res.sessionId,
              ueIp: res.ueIp,
              amfState: 'REGISTERED',
              sessionState: 'ACTIVE',
              ruleId: res.sessionId ? `RULE-${res.sessionId}` : undefined,
              packetsForwarded: 0,
              active: true,
            },
            ...prev,
          ]);
        }
      } catch (cause) {
        if (alive.current) setError(cause instanceof Error ? cause.message : '出错了，再试一次。');
      } finally {
        busy.current = false;
        if (alive.current) setRunning(false);
      }
    },
    [mode, updateDemoUes],
  );

  const handleDeregister = useCallback(
    async (id: string) => {
      if (mode === 'demo') {
        updateDemoUes((prev) => prev.filter((u) => u.ueId !== id));
        setNotice(`${id}：已从演示列表移除（排练数据）`);
        return;
      }
      try {
        const res = await apiDeregister(id);
        setNotice(`${res.ueId}：${res.message}`);
        const { ues: list } = await apiUEs();
        if (alive.current) setUes(list);
      } catch (cause) {
        setNotice(cause instanceof Error ? cause.message : '注销失败');
      }
    },
    [mode, updateDemoUes],
  );

  // 小压测：工作池并发注册 → 测完（计好墙钟）再串行注销清理 → 汇总写入最近 3 轮。
  // 所有计数用闭包局部变量累积，结束前一次性写回 state，避免 updater 副作用。
  const startLoadTest = useCallback(async () => {
    if (busy.current || mode !== 'live') return;
    const concurrency = Math.min(50, Math.max(1, Number.parseInt(loadInputs.concurrency, 10) || 1));
    const total = Math.min(200, Math.max(1, Number.parseInt(loadInputs.total, 10) || 1));
    const seq = ++loadSeq.current;
    busy.current = true;
    setError('');
    const startedAt = Date.now();
    const okIds: string[] = [];
    const latencyList: number[] = [];
    const pointList: LoadLatencyPoint[] = [];
    let okCount = 0;
    let failCount = 0;
    let nextIndex = 0;
    const commit = () => {
      setLoadState({
        running: true,
        total,
        done: okCount + failCount,
        ok: okCount,
        fail: failCount,
        latencies: [...latencyList],
        points: [...pointList],
        startedAt,
        cleanupFail: 0,
      });
    };
    setLoadState({
      running: true,
      total,
      done: 0,
      ok: 0,
      fail: 0,
      latencies: [],
      points: [],
      startedAt,
      cleanupFail: 0,
    });
    const worker = async () => {
      for (;;) {
        const index = nextIndex++;
        if (index >= total) return;
        const id = `UE-LOAD-${seq}-${String(index + 1).padStart(3, '0')}`;
        try {
          const res = await apiRegister(id, scenario);
          if (!alive.current) return;
          const ms = Math.max(res.processingMs, 1);
          pointList.push({ i: index + 1, ms, ok: res.success });
          if (res.success) {
            okCount += 1;
            latencyList.push(res.processingMs);
            okIds.push(id);
          } else {
            failCount += 1;
          }
        } catch {
          if (!alive.current) return;
          failCount += 1;
          pointList.push({ i: index + 1, ms: Math.max(Date.now() - startedAt, 1), ok: false });
        }
        commit();
      }
    };
    const workers = Array.from({ length: Math.min(concurrency, total) }, () => worker());
    await Promise.all(workers);
    const wall = (Date.now() - startedAt) / 1000;
    // 清理刻意放在测量之后，注销 RPC 不污染延迟与吞吐数字。
    let cleanupFail = 0;
    for (const id of okIds) {
      try {
        await apiDeregister(id);
      } catch {
        cleanupFail += 1;
      }
    }
    if (!alive.current) return;
    const sorted = [...latencyList].sort((a, b) => a - b);
    const summary: LoadRunSummary = {
      seq,
      total,
      ok: okCount,
      fail: failCount,
      rate: total > 0 ? (okCount * 100) / total : 0,
      median: pct(sorted, 0.5),
      p95: pct(sorted, 0.95),
      max: sorted.length > 0 ? sorted[sorted.length - 1] : 0,
      throughput: wall > 0 ? total / wall : 0,
    };
    setLoadRuns((prev) => [...prev, summary].slice(-3));
    setLoadState({
      running: false,
      total,
      done: okCount + failCount,
      ok: okCount,
      fail: failCount,
      latencies: [...latencyList],
      points: [...pointList],
      startedAt,
      finishedAt: Date.now(),
      cleanupFail,
    });
    if (cleanupFail > 0) {
      setNotice(`压测完成：${cleanupFail} 个终端注销清理失败，可到「在线 UE」面板手动注销。`);
    }
    try {
      const { ues: list } = await apiUEs();
      if (alive.current) setUes(list);
    } catch {
      /* 列表会由轮询兜底 */
    }
    busy.current = false;
  }, [loadInputs, mode, scenario]);

  const shownUEs = mode === 'demo' ? demoUes : ues;
  const loadBusy = !!loadState?.running;
  const anyWarn = mode === 'live' && !!health && health.services.some((service) => service.status === 'NOT_SERVING');

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">跳到实验工作区</a>
      <header className="masthead">
        <div className="masthead-top">
          <div className="brand">
            <span className="brand-mark" aria-hidden="true">LC</span>
            <span className="brand-copy">
              <strong>LiteCore</strong>
              <span>5G 核心网实验台</span>
            </span>
          </div>
          <span className={`mode-badge ${mode}`}>
            <span className="mode-indicator" aria-hidden="true" />
            {mode === 'live' ? '实时连接' : mode === 'demo' ? '演示模式' : '正在连接'}
          </span>
        </div>
        <div className="masthead-status">
          <p className={`health-line${anyWarn ? ' warn' : ''}`}>{healthLine(mode, health)}</p>
          <HealthDots mode={mode} health={health} />
        </div>
      </header>

      {mode === 'demo' && (
        <div className="demo-banner" role="status">
          <span className="banner-mark" aria-hidden="true">i</span>
          <span>
            <strong>正在使用演示数据</strong>
            网关暂未连接。启动 AMF、SMF、UPF 和 Gateway 后，页面会自动恢复实时数据。
          </span>
        </div>
      )}

      <main className="workspace" id="main-content">
        <div className="page-heading">
          <div>
            <h1>5G 核心网注册实验</h1>
            <p>选择信道并发起注册，观察 UE 如何经过 AMF、SMF 与 UPF 建立会话。</p>
          </div>
          <div className="context-note" aria-label="注册路径">
            <span>UE</span><i aria-hidden="true" /><span>AMF</span><i aria-hidden="true" /><span>SMF</span><i aria-hidden="true" /><span>UPF</span>
          </div>
        </div>

        <section className="workbench" aria-label="注册实验工作台">
          <section className="register-surface" aria-labelledby="register-title">
            <div className="section-heading">
              <div>
                <h2 id="register-title">实验参数</h2>
                <p>配置本次 UE 注册请求</p>
              </div>
              <span className="section-tag">{mode === 'live' ? '真实请求' : '排练数据'}</span>
            </div>
            <form
              aria-busy={running}
              onSubmit={(event) => {
                event.preventDefault();
                void run(ueId.trim(), scenario);
              }}
            >
              <label htmlFor="ue-id">终端标识</label>
              <input
                id="ue-id"
                className="field"
                value={ueId}
                maxLength={48}
                disabled={running || loadBusy || mode === 'probing'}
                onChange={(event) => {
                  setUeId(event.target.value);
                  setError('');
                }}
                aria-invalid={!!error}
                aria-describedby={error ? 'form-error' : undefined}
                autoComplete="off"
              />
              <label htmlFor="scenario">信道场景</label>
              <select
                id="scenario"
                className="field"
                value={scenario}
                aria-describedby="scenario-help"
                disabled={running || loadBusy || mode === 'probing'}
                onChange={(event) => {
                  const value = event.target.value as Scenario;
                  if (Object.hasOwn(SCENARIOS, value)) setScenario(value);
                }}
              >
                {Object.entries(SCENARIOS).map(([key, value]) => (
                  <option key={key} value={key}>
                    {value.label}
                  </option>
                ))}
              </select>
              <p className="field-help" id="scenario-help">
                {SCENARIOS[scenario].description}
              </p>
              {error && (
                <p id="form-error" className="form-error" role="alert">
                  {error}
                </p>
              )}
              <button className="run-button" type="submit" disabled={running || loadBusy || mode === 'probing'}>
                <span className="button-status" aria-hidden="true" />
                {running ? '注册进行中' : '发起注册'}
              </button>
            </form>
          </section>

          <section className="flow-surface" aria-labelledby="flow-title">
            <div className="section-heading flow-heading">
              <div>
                <h2 id="flow-title">注册路径</h2>
                <p>请求经过四个节点完成接入</p>
              </div>
              <span className="protocol-label">gRPC</span>
            </div>

            <div className={`flow-summary ${running ? 'working' : result ? (result.success ? 'ok' : 'bad') : ''}`}>
              <span className="state-dot" aria-hidden="true" />
              <strong>{flowStatus(phase, running, result)}</strong>
            </div>

            <ol className="network-path" aria-label="注册流程">
              {NODES.map((node, index) => {
                const reached = phase >= index;
                const completed = reached && (!running || phase > index);
                const rejected = result !== null && !result.success && index === 1;
                const cls = [
                  reached ? 'reached' : '',
                  completed ? 'completed' : '',
                  running && phase === index ? 'current' : '',
                  rejected ? 'stopped' : '',
                ]
                  .filter(Boolean)
                  .join(' ');
                return (
                  <li key={node.id} className={cls}>
                    <div className="node-symbol">
                      <span>{node.id}</span>
                      {(completed || rejected) && <i className="node-result" aria-hidden="true" />}
                    </div>
                    <strong>{node.id}</strong>
                    <span>{node.name}</span>
                    <small>{node.port}</small>
                    <span className="sr-only">
                      {rejected ? '注册在此节点被拒绝' : completed ? '已完成' : running && phase === index ? '处理中' : '等待'}
                    </span>
                    {index < NODES.length - 1 && (
                      <span className={`path-arrow ${phase > index ? 'passed' : running && phase === index ? 'active' : ''}`} aria-hidden="true" />
                    )}
                  </li>
                );
              })}
            </ol>

            <div className="measurement-grid" aria-label="接入门控测量">
              <div className={`measurement ${result ? (result.signalPower >= -110 ? 'ok' : 'bad') : ''}`}>
                <div className="measurement-label">
                  <span>信号功率</span>
                  <small>门槛 ≥ −110 dBm</small>
                </div>
                <strong className="mono">{result ? `${result.signalPower} dBm` : '等待测量'}</strong>
                <div
                  className="meter signal-meter"
                  style={{ '--meter': result ? `${signalMeter(result.signalPower)}%` : '0%' } as CSSProperties}
                  aria-hidden="true"
                >
                  <span />
                  <i />
                </div>
              </div>
              <div className={`measurement ${result ? (result.sinr >= 0 ? 'ok' : 'bad') : ''}`}>
                <div className="measurement-label">
                  <span>SINR</span>
                  <small>门槛 ≥ 0 dB</small>
                </div>
                <strong className="mono">{result ? `${result.sinr} dB` : '等待测量'}</strong>
                <div
                  className="meter sinr-meter"
                  style={{ '--meter': result ? `${sinrMeter(result.sinr)}%` : '0%' } as CSSProperties}
                  aria-hidden="true"
                >
                  <span />
                  <i />
                </div>
              </div>
            </div>

            <div className="result-slot" aria-live="polite" aria-atomic="true">
              {result ? (
                <div className={`result ${result.success ? 'ok' : 'bad'}`}>
                  <div className="result-copy">
                    <strong>{resultHeadline(result)}</strong>
                    <p className="result-message">{result.message}</p>
                  </div>
                  {result.success && (
                    <dl className="result-detail">
                      <div>
                        <dt>AMF UE ID</dt>
                        <dd className="mono">{result.amfUeId}</dd>
                      </div>
                      <div>
                        <dt>会话 ID</dt>
                        <dd className="mono">{result.sessionId}</dd>
                      </div>
                      <div>
                        <dt>分配 IP</dt>
                        <dd className="mono">{result.ueIp}</dd>
                      </div>
                      <div>
                        <dt>处理耗时</dt>
                        <dd className="mono">{result.processingMs} ms</dd>
                      </div>
                    </dl>
                  )}
                  <GateChecklist signal={result.signalPower} sinr={result.sinr} />
                  <RetryTimeline retries={result.retries} success={result.success} />
                </div>
              ) : (
                <div className="result-empty">
                  <span className="empty-mark" aria-hidden="true" />
                  <p>{running ? '请求正在沿注册路径传递，请观察当前节点。' : '运行一次实验后，这里会显示门控测量、会话与 IP。'}</p>
                </div>
              )}
            </div>

            <p className="flow-footnote">接入门控仅在首次注册时生效；UPF 包数为教学用模拟计数。</p>
          </section>
        </section>

        <div className="data-grid">
          <section className="panel history-surface" aria-labelledby="history-title">
            <div className="section-heading">
              <div>
                <h2 id="history-title">实验记录</h2>
                <p>最近六次注册结果</p>
              </div>
              {records.length > 0 && (
                <button
                  className="ghost-button"
                  type="button"
                  disabled={running}
                  onClick={() => {
                    setRecords([]);
                    setResult(null);
                    setPhase(-1);
                  }}
                >
                  清空记录
                </button>
              )}
            </div>
            {records.length > 0 ? (
              <ul className="history-list">
                {records.map(({ key, r }) => (
                  <li key={key}>
                    <div className="history-primary">
                      <span className={`status-pill ${r.success ? 'ok' : 'bad'}`}>
                        <i aria-hidden="true" />
                        {r.success ? '注册成功' : '注册被拒'}
                      </span>
                      <strong className="mono">{r.ueId}</strong>
                    </div>
                    <div className="history-data">
                      <span>
                        <small>信号</small>
                        <strong className="mono">{r.signalPower} dBm</strong>
                      </span>
                      <span>
                        <small>SINR</small>
                        <strong className="mono">{r.sinr} dB</strong>
                      </span>
                      <span>
                        <small>IP</small>
                        <strong className="mono">{r.success ? r.ueIp : '未分配'}</strong>
                      </span>
                      <span className="hist-latency-cell">
                        <small>耗时{r.retries > 0 ? ` · 重试 ${r.retries}` : ''}</small>
                        <span
                          className="hist-latency"
                          title={r.processingMs > 0 ? `后端耗时 ${r.processingMs} ms` : undefined}
                        >
                          {r.processingMs > 0 ? (
                            <i className="hist-bar" style={{ width: `${Math.min(r.processingMs / 500, 1) * 100}%` }} />
                          ) : (
                            <em className="hist-demo">{mode === 'demo' ? '排练数据' : '—'}</em>
                          )}
                        </span>
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            ) : (
              <div className="empty-state">
                <span className="empty-mark" aria-hidden="true" />
                <p>还没有实验记录</p>
                <small>发起注册后，结果会保留在这里供你对比不同信道。</small>
              </div>
            )}
          </section>

          <section className="panel ues-surface" aria-labelledby="ues-title">
            <div className="section-heading">
              <div>
                <h2 id="ues-title">在线 UE</h2>
                <p>会话状态与 UPF 转发计数</p>
              </div>
              <span className="record-count" aria-label={`${shownUEs.length} 台在线 UE`}>{shownUEs.length}</span>
            </div>
            {notice && (
              <p className="notice" role="status">
                {notice}
              </p>
            )}
            {shownUEs.length > 0 ? (
              <div className="table-wrap">
                <table className="ues-table">
                  <caption className="sr-only">在线 UE 列表</caption>
                  <thead>
                    <tr>
                      <th>UE</th>
                      <th>IP</th>
                      <th>会话</th>
                      <th className="numeric">转发包数</th>
                      <th className="numeric">趋势</th>
                      <th><span className="sr-only">操作</span></th>
                    </tr>
                  </thead>
                  <tbody>
                    {shownUEs.map((u) => {
                      const expanded = expandedUE === u.ueId;
                      return (
                        <Fragment key={u.ueId}>
                          <tr>
                            <td data-label="UE">
                              <button
                                className="ue-expand mono"
                                type="button"
                                aria-expanded={expanded}
                                onClick={() => setExpandedUE(expanded ? null : u.ueId)}
                              >
                                <span className="caret" aria-hidden="true">{expanded ? '▾' : '▸'}</span>
                                {u.ueId}
                              </button>
                            </td>
                            <td className="mono" data-label="IP">{u.ueIp ?? '—'}</td>
                            <td data-label="会话">
                              <span className={`status-text ${u.sessionState === 'ACTIVE' || mode === 'demo' ? 'ok' : 'bad'}`}>
                                <i aria-hidden="true" />
                                {u.sessionState ?? '—'}
                              </span>
                            </td>
                            <td className="numeric mono" data-label="转发包数">{u.packetsForwarded.toLocaleString()}</td>
                            <td className="numeric" data-label="趋势">
                              <PacketSparkline values={packetSpark[u.ueId] ?? []} />
                            </td>
                            <td data-label="操作">
                              <button className="ghost-button small" type="button" onClick={() => void handleDeregister(u.ueId)}>
                                注销
                              </button>
                            </td>
                          </tr>
                          {expanded && (
                            <tr className="ue-resource-row">
                              <td colSpan={6}>
                                <UEResourceDetail ue={u} />
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-state">
                <span className="empty-mark" aria-hidden="true" />
                <p>当前没有在线 UE</p>
                <small>成功注册后，UE 的会话和转发计数会显示在这里。</small>
              </div>
            )}
          </section>
        </div>

        <div className="data-grid">
          <section className="panel scatter-surface" aria-labelledby="scatter-title">
            <div className="section-heading">
              <div>
                <h2 id="scatter-title">注册信号散点</h2>
                <p>每次注册的信道采样，叠加两道接入门限</p>
              </div>
              <div className="heading-actions">
                <span className="section-tag">
                  {samples.length} 个样本{mode === 'demo' && ' · 排练数据'}
                </span>
                {samples.length > 0 && (
                  <button className="ghost-button small" type="button" onClick={() => setSamples([])}>
                    清空散点
                  </button>
                )}
              </div>
            </div>
            <SignalScatter samples={samples} />
            <p className="flow-footnote">右上方象限（信号 ≥ −110 dBm 且 SINR ≥ 0）才会被 AMF 放行，其余三个象限都会被拒绝。</p>
          </section>

          <section className="panel packet-surface" aria-labelledby="packet-title">
            <div className="section-heading">
              <div>
                <h2 id="packet-title">用户面吞吐（模拟）</h2>
                <p>在线 UE 转发包数之和 · 每 1.5 秒采样</p>
              </div>
              <span className="section-tag">只增不减</span>
            </div>
            <PacketCurve series={packetSeries} />
            <p className="flow-footnote">终端注销后，它的历史包数并入累计值，曲线不回退。UPF 每台每秒记 10 包（教学模拟，不是真实吞吐）。</p>
          </section>
        </div>

        <section className="panel health-history-surface" aria-labelledby="health-title">
          <div className="section-heading">
            <div>
              <h2 id="health-title">服务心跳历史</h2>
              <p>每格 3 秒 · 保留 6 分钟 · 橙格 = 优雅退出中，红格 = 连不上</p>
            </div>
            <span className="section-tag">可用性</span>
          </div>
          <HealthHistoryStrip snapshots={healthSnapshots} paused={mode !== 'live'} />
        </section>

        <section className="panel load-surface" aria-labelledby="load-title">
          <div className="section-heading">
            <div>
              <h2 id="load-title">并发注册小压测</h2>
              <p>前端驱动的批量注册 · 走正式注册通道 · 测完自动注销</p>
            </div>
            <span className="section-tag">结果仪表区</span>
          </div>
          <LoadTestPanel
            state={loadState}
            runs={loadRuns}
            inputs={loadInputs}
            onInput={(field, value) => setLoadInputs((prev) => ({ ...prev, [field]: value }))}
            onStart={() => void startLoadTest()}
            disabled={mode !== 'live' || running || loadBusy}
            demo={mode === 'demo'}
          />
        </section>
      </main>

      <footer>
        <span>LiteCore · 轻量级 5G 核心网教学实验</span>
        <span>{mode === 'live' ? '数据来自 LiteCore 后端' : '当前显示排练数据'} · UPF 转发为模拟计数</span>
      </footer>
    </div>
  );
}
