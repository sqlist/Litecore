// 行内迷你火花线：单台 UE 最近 40 次包计数采样的走势（84×20 小图，无轴无字）。
import { lin } from './scale';

const W = 84;
const H = 20;

export function PacketSparkline({ values }: { values: number[] }) {
  if (values.length < 2) return <span className="sparkline-empty">—</span>;
  let min = Math.min(...values);
  let max = Math.max(...values);
  if (max === min) {
    max += 1;
    min -= 1;
  }
  const points = values
    .map((v, i) => `${lin(i, { min: 0, max: values.length - 1 }, { min: 0, max: W })},${lin(v, { min, max }, { min: H - 1, max: 1 })}`)
    .join(' ');
  const last = values[values.length - 1];
  return (
    <svg className="sparkline" viewBox={`0 0 ${W} ${H}`} aria-hidden="true">
      <title>{`近 40 次采样 · 最新 ${last.toLocaleString()} 包`}</title>
      <polyline points={points} fill="none" stroke="var(--accent)" strokeWidth="1.2" />
    </svg>
  );
}
