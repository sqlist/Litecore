// 网页版小压测面板：表单 + 结果仪表区。
// 编排（工作池、清理、汇总）在 App.tsx 的 startLoadTest()，这里只做展示。
import { LatencyEvolution, type LoadLatencyPoint } from './LatencyEvolution';
import { LatencyHistogram, pct } from './LatencyHistogram';
import { CompareBars, SuccessBar } from './SuccessBar';

export type LoadRunState = {
  running: boolean;
  total: number;
  done: number;
  ok: number;
  fail: number;
  latencies: number[]; // 成功请求的耗时（ms），直方图与分位数的口径
  points: LoadLatencyPoint[]; // 完成顺序上的每一个请求（成败都记）
  startedAt: number;
  finishedAt?: number;
  cleanupFail: number;
};

export type LoadRunSummary = {
  seq: number;
  total: number;
  ok: number;
  fail: number;
  rate: number; // 成功率 %
  median: number;
  p95: number;
  max: number;
  throughput: number; // 个/秒
};

type Props = {
  state: LoadRunState | null;
  runs: LoadRunSummary[];
  inputs: { concurrency: string; total: string };
  onInput: (field: 'concurrency' | 'total', value: string) => void;
  onStart: () => void;
  disabled: boolean;
  demo: boolean;
};

// 从当前状态实时推导 6 个统计瓦片的数值（运行时用墙钟，结束后用 finishedAt）。
function liveStat(state: LoadRunState) {
  const elapsed = ((state.finishedAt ?? Date.now()) - state.startedAt) / 1000;
  const sorted = [...state.latencies].sort((a, b) => a - b);
  return {
    rate: state.total > 0 ? (state.ok * 100) / state.total : 0,
    median: pct(sorted, 0.5),
    p95: pct(sorted, 0.95),
    max: sorted.length > 0 ? sorted[sorted.length - 1] : 0,
    throughput: elapsed > 0 ? state.done / elapsed : 0,
  };
}

export function LoadTestPanel({ state, runs, inputs, onInput, onStart, disabled, demo }: Props) {
  return (
    <div className="load-panel">
      <div className="load-form">
        <label htmlFor="load-concurrency">并发数（1–50）</label>
        <input
          id="load-concurrency"
          className="field load-input"
          inputMode="numeric"
          value={inputs.concurrency}
          disabled={disabled || !!state?.running}
          onChange={(event) => onInput('concurrency', event.target.value)}
        />
        <label htmlFor="load-total">总数（1–200）</label>
        <input
          id="load-total"
          className="field load-input"
          inputMode="numeric"
          value={inputs.total}
          disabled={disabled || !!state?.running}
          onChange={(event) => onInput('total', event.target.value)}
        />
        <button className="run-button load-button" type="button" disabled={disabled || !!state?.running} onClick={onStart}>
          {state?.running ? `压测中 ${state.done}/${state.total}…` : '开始压测'}
        </button>
        <p className="field-help">
          ID 形如 UE-LOAD-1-001，走正式注册通道；复用主表单的信道场景。结束后自动逐个注销清理，不污染实验数据。
        </p>
      </div>
      {demo && <p className="hint">演示模式下不发真实请求——压测需要连上后端，等页面自动切回实时模式再试。</p>}
      {state ? (
        <LoadDashboard state={state} runs={runs} />
      ) : (
        <p className="hint">还没压测过。输入并发与总数，点「开始压测」——结果仪表区会实时成形，结束后保留最近一轮。</p>
      )}
    </div>
  );
}

// 仪表区单独成组件：state 一定非空，统计推导不需要判空。
function LoadDashboard({ state, runs }: { state: LoadRunState; runs: LoadRunSummary[] }) {
  const stats = liveStat(state);
  return (
    <div className="load-dashboard">
      <div className="load-stats">
        <dl className="load-stat">
          <dt>总请求</dt>
          <dd>
            {state.done}/{state.total}
          </dd>
        </dl>
        <dl className="load-stat">
          <dt>成功率</dt>
          <dd>{stats.rate.toFixed(1)}%</dd>
        </dl>
        <dl className="load-stat">
          <dt>中位数</dt>
          <dd>{state.latencies.length > 0 ? `${stats.median.toFixed(1)} ms` : '—'}</dd>
        </dl>
        <dl className="load-stat">
          <dt>P95</dt>
          <dd>{state.latencies.length > 0 ? `${stats.p95.toFixed(1)} ms` : '—'}</dd>
        </dl>
        <dl className="load-stat">
          <dt>最大</dt>
          <dd>{state.latencies.length > 0 ? `${stats.max.toFixed(1)} ms` : '—'}</dd>
        </dl>
        <dl className="load-stat">
          <dt>吞吐</dt>
          <dd>{stats.throughput.toFixed(1)}/s</dd>
        </dl>
      </div>
      {state.cleanupFail > 0 && (
        <p className="load-cleanup-warn">注销清理失败 {state.cleanupFail} 个——可到「在线终端」面板手动注销。</p>
      )}
      <LatencyEvolution points={state.points} total={state.total} running={state.running} />
      <div className="load-charts">
        <div className="load-chart-block">
          <h3>延迟分布（成功注册）</h3>
          <LatencyHistogram latencies={state.latencies} />
        </div>
        <div className="load-chart-block">
          <h3>成败占比</h3>
          <SuccessBar ok={state.ok} fail={state.fail} />
          <p className="hint">失败多数是门控拒绝；网关断连时在飞请求会全部计失败。</p>
          {runs.length > 0 && (
            <>
              <h3>最近 {runs.length} 轮对比</h3>
              <CompareBars runs={runs} />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
