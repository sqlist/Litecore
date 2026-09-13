// 延迟直方图：成功注册的耗时落入固定 8 桶，柱顶标计数。
// 桶边界 [0,100,200,300,500,800,1200,2000,∞) 毫秒。
import { lin } from './scale';

// 分位数（同 ue/benchmark.go 的 durationPercentile 口径：向上取整索引）。
export function pct(sorted: number[], p: number): number {
  if (sorted.length === 0) return 0;
  const index = Math.min(sorted.length - 1, Math.max(0, Math.ceil(p * sorted.length) - 1));
  return sorted[index];
}

const BUCKETS = [
  { label: '0–100', max: 100 },
  { label: '100–200', max: 200 },
  { label: '200–300', max: 300 },
  { label: '300–500', max: 500 },
  { label: '500–800', max: 800 },
  { label: '800–1200', max: 1200 },
  { label: '1200–2000', max: 2000 },
  { label: '2000+', max: Infinity },
];

const W = 600;
const H = 160;
const PAD_B = 20;
const PAD_T = 10;

export function LatencyHistogram({ latencies }: { latencies: number[] }) {
  if (latencies.length === 0) return <p className="hint">还没有成功请求，直方图在等数据。</p>;
  const counts = BUCKETS.map((bucket, i) =>
    latencies.filter((l) => l > (i === 0 ? -Infinity : BUCKETS[i - 1].max) && l <= bucket.max).length,
  );
  const maxCount = Math.max(1, ...counts);
  const barW = W / BUCKETS.length;
  return (
    <svg className="viz-svg" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="成功注册延迟直方图">
      {counts.map((count, i) => {
        const h = lin(count, { min: 0, max: maxCount }, { min: 0, max: H - PAD_B - PAD_T });
        const x = i * barW + 2;
        return (
          <g key={BUCKETS[i].label}>
            <rect x={x} y={H - PAD_B - h} width={barW - 4} height={h} rx={2} fill="var(--accent)" opacity={0.85} />
            {count > 0 && (
              <text x={x + (barW - 4) / 2} y={H - PAD_B - h - 3} textAnchor="middle" className="tick" fill="var(--muted)">
                {count}
              </text>
            )}
            <text x={x + (barW - 4) / 2} y={H - 6} textAnchor="middle" className="tick" fill="var(--faint)">
              {BUCKETS[i].label}
            </text>
          </g>
        );
      })}
    </svg>
  );
}
