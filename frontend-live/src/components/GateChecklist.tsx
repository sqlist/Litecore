// 门控判定清单：把 AMF 的两道接入门限逐项打勾/打叉。
// 阈值与 amf/handler.go 保持一致：信号 ≥ −110 dBm 且 SINR ≥ 0 dB。
export function GateChecklist({ signal, sinr }: { signal: number; sinr: number }) {
  const signalOk = signal >= -110;
  const sinrOk = sinr >= 0;
  return (
    <div className="gate-checklist">
      <div className={`gate-check ${signalOk ? 'ok' : 'bad'}`}>
        <span className="gate-mark">{signalOk ? '✓' : '✗'}</span>
        <span className="gate-label">信号功率 ≥ −110 dBm</span>
        <span className="gate-value mono">{signal.toFixed(2)} dBm</span>
      </div>
      <div className={`gate-check ${sinrOk ? 'ok' : 'bad'}`}>
        <span className="gate-mark">{sinrOk ? '✓' : '✗'}</span>
        <span className="gate-label">SINR ≥ 0 dB</span>
        <span className="gate-value mono">{sinr.toFixed(2)} dB</span>
      </div>
    </div>
  );
}
