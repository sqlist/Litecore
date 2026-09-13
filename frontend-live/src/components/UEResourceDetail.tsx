// 三方资源台账：一台 UE 在三张表（AMF / SMF / UPF）里各自留下了什么。
// 数据全部来自 /api/ues 的真实返回，缺省显示 —。
import type { UEEntry } from '../api';

export function UEResourceDetail({ ue }: { ue: UEEntry }) {
  return (
    <dl className="ue-resource-detail">
      <div className="resource-tile">
        <dt>AMF 记录</dt>
        <dd>
          <span>UE ID</span>
          <span className="mono">{ue.amfUeId ?? '—'}</span>
        </dd>
        <dd>
          <span>状态</span>
          <span>{ue.amfState ?? '—'}</span>
        </dd>
      </div>
      <div className="resource-tile">
        <dt>SMF 会话</dt>
        <dd>
          <span>会话</span>
          <span className="mono">{ue.sessionId ?? '—'}</span>
        </dd>
        <dd>
          <span>状态</span>
          <span>{ue.sessionState ?? '—'}</span>
        </dd>
      </div>
      <div className="resource-tile">
        <dt>UPF 规则</dt>
        <dd>
          <span>规则</span>
          <span className="mono">{ue.ruleId ?? '—'}</span>
        </dd>
        <dd>
          <span>活跃</span>
          <span>{ue.active ? '是' : '否'}</span>
        </dd>
      </div>
    </dl>
  );
}
