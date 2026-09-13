// 延迟演化散点图：x = 完成序号，y = 耗时（log 刻度 1~10000 ms）。
// 压测运行时点逐颗出现：首批请求通常偏慢（预热），之后趋于平稳；失败点用红点标出。
import { lin } from './scale';

export type LoadLatencyPoint = { i: number; ms: number; ok: boolean };

const W = 600;
const H = 220;
const PAD_L = 46;
const PAD_R = 12;
const PAD_T = 12;
const PAD_B = 26;
const LOG_MIN = 0; // 1 ms
const LOG_MAX = 4; // 10000 ms

const GRID_MS = [1, 10, 100, 1000, 10000];

export function LatencyEvolution({
  points,
  total,
  running,
}: {
  points: LoadLatencyPoint[];
  total: number;
  running: boolean;
}) {
  const x = (i: number) => lin(i, { min: 1, max: Math.max(1, total) }, { min: PAD_L, max: W - PAD_R });
  const y = (ms: number) => {
    const clamped = Math.max(ms, 1);
    return lin(Math.log10(clamped), { min: LOG_MIN, max: LOG_MAX }, { min: H - PAD_B, max: PAD_T });
  };
  if (!running && points.length === 0) {
    return <p className="hint">还没有压测数据。运行一次后，每个请求的耗时都会落在这里。</p>;
  }
  return (
    <svg className="viz-svg" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="压测延迟演化散点">
      {GRID_MS.map((ms) => (
        <g key={ms}>
          <line x1={PAD_L} x2={W - PAD_R} y1={y(ms)} y2={y(ms)} stroke="var(--border)" strokeWidth="1" strokeDasharray="3 4" />
          <text x={PAD_L - 6} y={y(ms) + 3} textAnchor="end" className="tick" fill="var(--faint)">
            {ms}
          </text>
        </g>
      ))}
      {points.map((p) => (
        <circle key={p.i} cx={x(p.i)} cy={y(p.ms)} r={2.5} fill={p.ok ? 'var(--ok)' : 'var(--bad)'} opacity={0.85}>
          <title>{`第 ${p.i} 个 · ${p.ok ? `成功 · ${p.ms.toFixed(1)} ms` : `失败 · 发起后 ${p.ms.toFixed(0)} ms`}`}</title>
        </circle>
      ))}
      <text x={12} y={H / 2} textAnchor="middle" transform={`rotate(-90 12 ${H / 2})`} className="axis-name" fill="var(--muted)">
        耗时 ms（log）
      </text>
      <text x={PAD_L} y={H - 4} className="tick" fill="var(--faint)">
        第 1 个
      </text>
      <text x={(PAD_L + W - PAD_R) / 2} y={H - 4} textAnchor="middle" className="tick" fill="var(--faint)">
        {running ? `已完成 ${points.length} / ${total}` : `共 ${points.length} 个`}
      </text>
      <text x={W - PAD_R} y={H - 4} textAnchor="end" className="tick" fill="var(--faint)">
        第 {total} 个
      </text>
    </svg>
  );
}
