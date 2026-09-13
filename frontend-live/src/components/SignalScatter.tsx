// 注册信号散点图：每次注册的信道采样落一个点，叠加两道接入门线。
// 固定坐标域（X = 信号 −130~−70 dBm，Y = SINR −10~30 dB），不同场景的点可直接对比。
import type { Scenario } from '../api';
import { lin } from './scale';

export type ScatterSample = {
  t: number; // 第几次采样
  signal: number;
  sinr: number;
  ok: boolean;
  scenario: Scenario;
};

const W = 600;
const H = 300;
const PAD_L = 46;
const PAD_R = 14;
const PAD_T = 12;
const PAD_B = 30;
const X_MIN = -130;
const X_MAX = -70;
const Y_MIN = -10;
const Y_MAX = 30;

const SCENARIO_NAMES: Record<Scenario, string> = {
  stable: '稳定',
  edge: '边缘',
  degrading: '弱信号',
  mixed: '混合',
};

const SCENARIO_ORDER = Object.keys(SCENARIO_NAMES) as Scenario[];

export function SignalScatter({ samples }: { samples: ScatterSample[] }) {
  const x = (v: number) => lin(v, { min: X_MIN, max: X_MAX }, { min: PAD_L, max: W - PAD_R });
  const y = (v: number) => lin(v, { min: Y_MIN, max: Y_MAX }, { min: H - PAD_B, max: PAD_T });
  const gateX = x(-110);
  const gateY = y(0);
  const ticksX = [-130, -120, -110, -100, -90, -80, -70];
  const ticksY = [-10, 0, 10, 20, 30];

  const counts: Record<Scenario, number> = { stable: 0, edge: 0, degrading: 0, mixed: 0 };
  for (const s of samples) counts[s.scenario] += 1;
  const total = samples.length;

  return (
    <div>
      <svg className="viz-svg" viewBox={`0 0 ${W} ${H}`} role="img" aria-label="注册信号与 SINR 散点图">
        {/* 四个象限的注记：只有右上方（两项都满足）才会被放行 */}
        <text x={PAD_L + 6} y={PAD_T + 14} className="tick" fill="var(--faint)">
          信号不足 · 拒绝
        </text>
        <text x={W - PAD_R - 6} y={PAD_T + 14} className="tick" fill="var(--ok)" textAnchor="end">
          两项满足 · 通过
        </text>
        <text x={PAD_L + 6} y={H - PAD_B - 6} className="tick" fill="var(--faint)">
          两项不足 · 拒绝
        </text>
        <text x={W - PAD_R - 6} y={H - PAD_B - 6} className="tick" fill="var(--bad)" textAnchor="end">
          SINR 不足 · 拒绝
        </text>
        {/* 两道门线 */}
        <line x1={gateX} x2={gateX} y1={PAD_T} y2={H - PAD_B} stroke="var(--accent)" strokeWidth="1" strokeDasharray="5 4" />
        <line x1={PAD_L} x2={W - PAD_R} y1={gateY} y2={gateY} stroke="var(--accent)" strokeWidth="1" strokeDasharray="5 4" />
        {/* 轴刻度 */}
        {ticksX.map((t) => (
          <g key={`x-${t}`}>
            <line x1={x(t)} x2={x(t)} y1={H - PAD_B} y2={H - PAD_B + 4} stroke="var(--border)" />
            <text x={x(t)} y={H - 10} textAnchor="middle" className="tick" fill="var(--faint)">
              {t}
            </text>
          </g>
        ))}
        {ticksY.map((t) => (
          <g key={`y-${t}`}>
            <line x1={PAD_L - 4} x2={PAD_L} y1={y(t)} y2={y(t)} stroke="var(--border)" />
            <text x={PAD_L - 8} y={y(t) + 3} textAnchor="end" className="tick" fill="var(--faint)">
              {t}
            </text>
          </g>
        ))}
        {/* 样本点 */}
        {samples.map((s) => (
          <circle
            key={s.t}
            cx={x(s.signal)}
            cy={y(s.sinr)}
            r={3.5}
            fill={s.ok ? 'var(--ok)' : 'var(--bad)'}
            opacity={0.85}
          >
            <title>{`#${s.t} ${SCENARIO_NAMES[s.scenario]} · ${s.signal.toFixed(2)} dBm · SINR ${s.sinr.toFixed(2)} dB · ${s.ok ? '通过' : '拒绝'}`}</title>
          </circle>
        ))}
        {/* 轴名 */}
        <text x={12} y={H / 2} textAnchor="middle" transform={`rotate(-90 12 ${H / 2})`} className="axis-name" fill="var(--muted)">
          SINR dB
        </text>
        <text x={(PAD_L + W - PAD_R) / 2} y={H - 2} textAnchor="middle" className="axis-name" fill="var(--muted)">
          信号功率 dBm（右强左弱）
        </text>
      </svg>
      {total === 0 && <p className="hint scatter-empty">暂无样本：每完成一次注册，信道采样就会在这里落一个点。</p>}
      {/* 场景分布小条：从样本派生，不用新状态 */}
      <div className="scenario-dist" aria-hidden="true">
        {SCENARIO_ORDER.map((key, i) => (
          <span
            key={key}
            className="scenario-seg"
            style={{
              width: total > 0 ? `${(counts[key] / total) * 100}%` : '0%',
              opacity: 0.35 + i * 0.2,
            }}
          >
            {counts[key] > 0 ? counts[key] : ''}
          </span>
        ))}
      </div>
      <div className="scenario-legend">
        {SCENARIO_ORDER.map((key, i) => (
          <span key={key} className="scenario-legend-item">
            <i className="scenario-swatch" style={{ opacity: 0.35 + i * 0.2 }} aria-hidden="true" />
            {SCENARIO_NAMES[key]} {counts[key]}
          </span>
        ))}
      </div>
    </div>
  );
}
