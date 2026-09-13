// 包计数聚合曲线：所有在线终端的转发包数之和随时间上升。
// 流失的终端会并入累计值（见 App.tsx 的 carry 逻辑），所以曲线只增不减。
import { lin, niceCeil } from './scale';

export type PacketPoint = { t: number; total: number };

const W = 600;
const H = 200;
const PAD_L = 52;
const PAD_R = 12;
const PAD_T = 14;
const PAD_B = 26;

export function PacketCurve({ series }: { series: PacketPoint[] }) {
  const maxValue = niceCeil(Math.max(1, ...series.map((p) => p.total)));
  const x = (i: number) =>
    lin(i, { min: 0, max: Math.max(1, series.length - 1) }, { min: PAD_L, max: W - PAD_R });
  const y = (v: number) => lin(v, { min: 0, max: maxValue }, { min: H - PAD_B, max: PAD_T });
  const points = series.map((p, i) => `${x(i)},${y(p.total)}`).join(' ');
  const last = series[series.length - 1];
  const ticks = [0, maxValue / 2, maxValue].map((v) => Math.round(v));
  const t0 = series[0]?.t ?? 0;
  const lastT = last?.t ?? t0;
  const totalSec = Math.round((lastT - t0) / 1000);
  const midSec = Math.round(totalSec / 2);

  return (
    <svg className="viz-svg" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="聚合转发包数趋势">
      {ticks.map((t) => (
        <g key={t}>
          <line x1={PAD_L} x2={W - PAD_R} y1={y(t)} y2={y(t)} stroke="var(--border)" strokeWidth="1" strokeDasharray="3 4" />
          <text x={PAD_L - 6} y={y(t) + 3} textAnchor="end" className="tick" fill="var(--faint)">
            {t}
          </text>
        </g>
      ))}
      {series.length > 1 && (
        <polygon points={`${PAD_L},${H - PAD_B} ${points} ${W - PAD_R},${H - PAD_B}`} fill="var(--accent-soft)" />
      )}
      {series.length > 1 && <polyline points={points} fill="none" stroke="var(--accent)" strokeWidth="2" />}
      {last && (
        <g>
          <circle cx={x(series.length - 1)} cy={y(last.total)} r={3.5} fill="var(--accent)" />
          <text x={x(series.length - 1)} y={y(last.total) - 8} textAnchor="middle" className="tick" fill="var(--accent)">
            {last.total.toLocaleString()}
          </text>
        </g>
      )}
      <text x={12} y={H / 2} textAnchor="middle" transform={`rotate(-90 12 ${H / 2})`} className="axis-name" fill="var(--muted)">
        累计包数
      </text>
      <text x={PAD_L} y={H - 4} className="tick" fill="var(--faint)">
        −{totalSec}s
      </text>
      <text x={(PAD_L + W - PAD_R) / 2} y={H - 4} textAnchor="middle" className="tick" fill="var(--faint)">
        −{midSec}s
      </text>
      <text x={W - PAD_R} y={H - 4} textAnchor="end" className="tick" fill="var(--faint)">
        现在
      </text>
    </svg>
  );
}
