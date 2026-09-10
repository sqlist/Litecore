import { useCallback, useEffect, useRef, useState } from 'react';
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

type Mode = 'probing' | 'live' | 'demo';

const NODES = [
  { id: 'UE', name: '模拟终端', task: '发起注册', port: '浏览器' },
  { id: 'AMF', name: '接入管理', task: '校验信道与门控', port: ':50051' },
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
  return `${down.map((service) => service.name).join('、')} 联系不上。别慌——这正好可以拿来演示容错。`;
}

function HealthDots({ mode, health }: { mode: Mode; health: Health | null }) {
  const names = ['网关', 'AMF', 'SMF', 'UPF'];
  return (
    <div className="health-dots" role="status">
      {names.map((name) => {
        let status: string = 'probing';
        if (mode === 'live') {
          status = name === '网关' ? 'SERVING' : (health?.services.find((service) => service.name === name)?.status ?? 'probing');
        }
        const cls = status === 'SERVING' ? 'ok' : status === 'probing' ? 'probing' : 'bad';
        return <span key={name} className={`dot ${cls}`} title={`${name}：${status}`} aria-hidden="true" />;
      })}
      <span className="health-line">{healthLine(mode, health)}</span>
    </div>
  );
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
  const alive = useRef(true);
  const busy = useRef(false);
  const nextKey = useRef(0);

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

  // 实时模式：每 1.5 秒拉一次在线终端，每 3 秒敲一次健康检查。
  useEffect(() => {
    if (mode !== 'live') return;
    const pollUEs = async () => {
      try {
        const { ues: list } = await apiUEs();
        if (alive.current) setUes(list);
      } catch {
        /* 网络抖动时保留上一份数据 */
      }
    };
    const pollHealth = async () => {
      try {
        const h = await apiHealth();
        if (alive.current) setHealth(h);
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
  }, [mode]);

  // 演示模式：包数每秒 +10，让排练数据也“活着”；每 5 秒再敲一次网关，通了自动切回实时。
  useEffect(() => {
    if (mode !== 'demo') return;
    const packetTimer = window.setInterval(() => {
      setDemoUes((prev) => prev.map((u) => ({ ...u, packetsForwarded: u.packetsForwarded + 10 })));
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
  }, [mode]);

  // 顶部通知 8 秒后自动消失。
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(''), 8000);
    return () => window.clearTimeout(timer);
  }, [notice]);

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
        if (mode === 'live' && res.success) {
          try {
            const { ues: list } = await apiUEs();
            if (alive.current) setUes(list);
          } catch {
            /* 列表会由轮询兜底 */
          }
        }
        if (mode === 'demo' && res.success) {
          setDemoUes((prev) => [
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
    [mode],
  );

  const handleDeregister = useCallback(
    async (id: string) => {
      if (mode === 'demo') {
        setDemoUes((prev) => prev.filter((u) => u.ueId !== id));
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
    [mode],
  );

  const shownUEs = mode === 'demo' ? demoUes : ues;

  return (
    <div className="app-shell">
      <header className="masthead">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">LC</span>
          <strong>LiteCore</strong>
          <span>5G 实验室 · 实验台</span>
        </div>
        <div className="masthead-right">
          <HealthDots mode={mode} health={health} />
          <span className={`mode-badge ${mode}`}>
            {mode === 'live' ? '实时连接' : mode === 'demo' ? '演示模式' : '连接中…'}
          </span>
        </div>
      </header>

      {mode === 'demo' && (
        <div className="demo-banner" role="status">
          演示模式 · 网关没连上。启动 amf、smf、upf 和 gateway 后，页面会自动切回实时数据。
        </div>
      )}

      <main className="workspace">
        <div className="page-heading">
          <h1>从一次注册，看懂核心网。</h1>
          <p>选择信道，发起请求，跟着终端走完接入流程——每一步都是后端真实动作。</p>
          <span className="context-note">UE → AMF → SMF → UPF</span>
        </div>

        <div className="grid">
          <section className="panel register-surface" aria-labelledby="register-title">
            <div className="section-heading">
              <h2 id="register-title">试一次注册</h2>
              <span>{mode === 'live' ? '真实调用后端' : '前端排练数据'}</span>
            </div>
            <form
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
                disabled={running || mode === 'probing'}
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
                disabled={running || mode === 'probing'}
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
              <button className="run-button" type="submit" disabled={running || mode === 'probing'}>
                {running ? '注册进行中…' : '发起注册'}
              </button>
            </form>

            <div className="result-slot" aria-live="polite" aria-atomic="true">
              {result ? (
                <div className={`result ${result.success ? 'ok' : 'bad'}`}>
                  <strong>{resultHeadline(result)}</strong>
                  <p className="result-message" title={result.message}>
                    {result.message}
                  </p>
                  {result.success && (
                    <dl className="result-detail">
                      <div>
                        <dt>AMF 起名</dt>
                        <dd className="mono">{result.amfUeId}</dd>
                      </div>
                      <div>
                        <dt>会话</dt>
                        <dd className="mono">{result.sessionId}</dd>
                      </div>
                      <div>
                        <dt>IP</dt>
                        <dd className="mono">{result.ueIp}</dd>
                      </div>
                    </dl>
                  )}
                  <p className="result-channel">
                    信号 {result.signalPower} dBm · SINR {result.sinr} dB
                    {result.retries > 0 && ` · 重试 ${result.retries} 次`}
                    {result.success && ` · 后端耗时 ${result.processingMs} ms`}
                  </p>
                </div>
              ) : (
                <p className="hint">
                  {running ? '请求正在路上，跟着上面的灯走。' : '试试「弱信号」，对比一次被拒绝的接入。'}
                </p>
              )}
            </div>

            <div className="history-heading">
              <h3>本次实验记录</h3>
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
                    <span className={`status-text ${r.success ? 'ok' : 'bad'}`}>
                      {r.success ? '✓ 注册成功' : '✕ 注册被拒'}
                    </span>
                    <strong className="mono">{r.ueId}</strong>
                    <span>
                      {r.signalPower} dBm · SINR {r.sinr} dB
                      {r.retries > 0 && ` · 重试 ${r.retries} 次`}
                    </span>
                    <span className="mono">{r.success ? r.ueIp : '未分配 IP'}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="hint">还没有记录。发起一次注册后，结果会留在这里，方便对比不同信道。</p>
            )}
          </section>

          <div className="side-stack">
            <section className="panel flow-surface" aria-labelledby="flow-title">
              <div className="section-heading">
                <h2 id="flow-title">注册路径</h2>
                <span>gRPC 服务架构</span>
              </div>
              <div className="flow-summary">
                <span className={`state-dot ${running ? 'working' : result ? (result.success ? 'ok' : 'bad') : ''}`} />
                <span>{flowStatus(phase, running, result)}</span>
              </div>
              <ol className="network-path" aria-label="注册流程">
                {NODES.map((node, index) => {
                  const reached = phase >= index;
                  const rejected = result !== null && !result.success && index === 1;
                  const cls = [
                    reached ? 'reached' : '',
                    running && phase === index ? 'current' : '',
                    rejected ? 'stopped' : '',
                  ]
                    .filter(Boolean)
                    .join(' ');
                  return (
                    <li key={node.id} className={cls}>
                      <div className="node-symbol">
                        <span>{node.id}</span>
                        <span className="node-result">
                          {rejected ? '✕' : reached && (!running || phase > index) ? '✓' : ''}
                        </span>
                      </div>
                      <strong>{node.id}</strong>
                      <span>{node.name}</span>
                      <small>{node.port}</small>
                      {index < NODES.length - 1 && (
                        <span className="path-arrow" aria-hidden="true">→</span>
                      )}
                    </li>
                  );
                })}
              </ol>
              <div className="gate-note">
                <strong>接入的两道门槛</strong>
                <p>
                  信号功率 ≥ −110 dBm，且 SINR ≥ 0 dB。任意一项不满足，AMF 就拒绝这次注册——而且只在第一次接入时检查。
                </p>
              </div>
              <p className="flow-footnote">节点状态实时来自网关；UPF 的包数是教学用模拟计数。</p>
            </section>

            <section className="panel ues-surface" aria-labelledby="ues-title">
              <div className="section-heading">
                <h2 id="ues-title">在线终端</h2>
                <span className="record-count">{shownUEs.length}</span>
              </div>
              {notice && (
                <p className="notice" role="status">
                  {notice}
                </p>
              )}
              {shownUEs.length > 0 ? (
                <table className="ues-table">
                  <thead>
                    <tr>
                      <th>终端</th>
                      <th>IP</th>
                      <th>会话</th>
                      <th className="numeric">转发包数</th>
                      <th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {shownUEs.map((u) => (
                      <tr key={u.ueId}>
                        <td className="mono">{u.ueId}</td>
                        <td className="mono">{u.ueIp ?? '—'}</td>
                        <td>
                          <span className={`status-text ${u.sessionState === 'ACTIVE' || mode === 'demo' ? 'ok' : 'bad'}`}>
                            {u.sessionState ?? '—'}
                          </span>
                        </td>
                        <td className="numeric mono">{u.packetsForwarded.toLocaleString()}</td>
                        <td>
                          <button className="ghost-button small" type="button" onClick={() => void handleDeregister(u.ueId)}>
                            注销
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              ) : (
                <div className="empty-ues">
                  <p>还没有终端连上来。发一次注册试试？</p>
                  <span>UPF 每秒钟给每台在线的 UE 记 10 个包——数字跳，就说明用户面活着。</span>
                </div>
              )}
            </section>
          </div>
        </div>
      </main>

      <footer>
        <span>LiteCore · 轻量级 5G 核心网实验</span>
        <span>实时连接 LiteCore 后端 · UPF 转发为模拟计数</span>
      </footer>
    </div>
  );
}
