// 服务心跳历史条：每服务一行 120 格时间条（每格 3 秒，共 6 分钟），
// 上方是每服务可用性百分比瓦片。NOT_SERVING = 优雅退出中（橙），UNREACHABLE = 连不上（红）。
export type HealthSnapshot = { t: number; statuses: Record<string, string> };

const ROWS = ['网关', 'AMF', 'SMF', 'UPF'];
const CELLS = 120;

function cellCls(status: string): string {
  if (status === 'SERVING') return 'ok';
  if (status === 'NOT_SERVING') return 'warn';
  if (status === 'UNREACHABLE') return 'bad';
  return 'probing';
}

export function HealthHistoryStrip({ snapshots, paused }: { snapshots: HealthSnapshot[]; paused: boolean }) {
  const uptime = (name: string): number | null => {
    if (snapshots.length === 0) return null;
    const serving = snapshots.filter(
      (s) => (name === '网关' ? 'SERVING' : s.statuses[name]) === 'SERVING',
    ).length;
    return (serving * 100) / snapshots.length;
  };
  return (
    <div>
      <div className="uptime-tiles">
        {ROWS.map((name) => {
          const pct = uptime(name);
          return (
            <div key={name} className={`uptime-tile ${pct === null ? '' : pct >= 99 ? 'ok' : 'warn'}`}>
              <span className="uptime-name">{name}</span>
              <span className="uptime-value">{pct === null ? '—' : `${pct.toFixed(1)}%`}</span>
            </div>
          );
        })}
        <div className="uptime-tile note">NOT_SERVING = 优雅退出中 · UNREACHABLE = 连不上</div>
      </div>
      {snapshots.length > 0 ? (
        <div className={`health-strip${paused ? ' paused' : ''}`}>
          {ROWS.map((name) => (
            <div key={name} className="health-row">
              <span className="health-row-name">{name}</span>
              <svg
                className="viz-svg health-cells"
                viewBox={`0 0 ${CELLS * 5} 14`}
                preserveAspectRatio="none"
                aria-hidden="true"
              >
                {snapshots.map((s, i) => {
                  const status = name === '网关' ? 'SERVING' : s.statuses[name] ?? 'probing';
                  return (
                    <rect key={`${s.t}-${name}`} x={i * 5} y={1} width={4} height={12} rx={1} className={`health-cell ${cellCls(status)}`}>
                      <title>{`${name} · ${new Date(s.t).toLocaleTimeString()} · ${status}`}</title>
                    </rect>
                  );
                })}
              </svg>
            </div>
          ))}
          <div className="health-axis">
            <span>6 分钟前</span>
            <span>现在</span>
          </div>
        </div>
      ) : (
        <p className="hint">还没有心跳记录。实时模式下每 3 秒记一格，最多保留 120 格（6 分钟）。</p>
      )}
      {paused && snapshots.length > 0 && (
        <p className="strip-paused">心跳记录暂停：网关没连上，上面是切换前的历史。恢复连接后自动续记。</p>
      )}
    </div>
  );
}
