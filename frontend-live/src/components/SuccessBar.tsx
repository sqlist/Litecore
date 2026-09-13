// 成败占比条 + 多轮对比小条形图（两者共用横向条的视觉语言）。
import type { LoadRunSummary } from './LoadTestPanel';

export function SuccessBar({ ok, fail }: { ok: number; fail: number }) {
  const total = ok + fail;
  if (total === 0) return null;
  const okPct = (ok * 100) / total;
  return (
    <div className="success-bar" role="img" aria-label={`成功 ${ok} 个，失败 ${fail} 个`}>
      <span className="success-bar-seg ok" style={{ width: `${okPct}%` }}>
        {ok > 0 ? ok : ''}
      </span>
      <span className="success-bar-seg bad" style={{ width: `${100 - okPct}%` }}>
        {fail > 0 ? fail : ''}
      </span>
    </div>
  );
}

export function CompareBars({ runs }: { runs: LoadRunSummary[] }) {
  if (runs.length === 0) return null;
  const shown = runs.slice(-3).reverse(); // 最新一轮在前
  const maxMedian = Math.max(1, ...shown.map((r) => r.median));
  return (
    <div className="compare-bars">
      <div className="compare-row">
        <span className="compare-dim">成功率</span>
        {shown.map((r) => (
          <div key={r.seq} className="compare-item">
            <span className="compare-bar-track">
              <span className="compare-bar ok" style={{ width: `${Math.min(100, Math.max(2, r.rate))}%` }} />
            </span>
            <span className="compare-num">{r.rate.toFixed(1)}%</span>
            <span className="compare-tag">第 {r.seq} 轮</span>
          </div>
        ))}
      </div>
      <div className="compare-row">
        <span className="compare-dim">中位数 ms</span>
        {shown.map((r) => (
          <div key={r.seq} className="compare-item">
            <span className="compare-bar-track">
              <span className="compare-bar accent" style={{ width: `${Math.min(100, Math.max(2, (r.median / maxMedian) * 100))}%` }} />
            </span>
            <span className="compare-num">{r.median.toFixed(0)}</span>
            <span className="compare-tag">第 {r.seq} 轮</span>
          </div>
        ))}
      </div>
    </div>
  );
}
