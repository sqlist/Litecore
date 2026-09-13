// 重试序列可视：retries > 0 时，把网关的指数退避重试画成时间线。
// 退避规则与 gateway 一致：每次等待 100ms·2^(n-1)。
export function RetryTimeline({ retries, success }: { retries: number; success: boolean }) {
  if (retries <= 0) return null;
  const attempts = Array.from({ length: retries }, (_, n) => ({
    n: n + 1,
    wait: 100 * 2 ** n,
  }));
  return (
    <div className="retry-timeline">
      <span className="retry-label">重试 {retries} 次</span>
      {attempts.map((attempt) => (
        <span key={attempt.n} className="retry-step">
          <span className="retry-mark bad">✗</span>
          <span>第 {attempt.n} 次</span>
          <span className="retry-arrow" aria-hidden="true">→</span>
          <span>等 {attempt.wait} ms</span>
          <span className="retry-arrow" aria-hidden="true">→</span>
        </span>
      ))}
      <span className="retry-step">
        <span className={`retry-mark ${success ? 'ok' : 'bad'}`}>{success ? '✓' : '✗'}</span>
        <span>第 {retries + 1} 次{success ? '成了' : '仍失败'}</span>
      </span>
    </div>
  );
}
