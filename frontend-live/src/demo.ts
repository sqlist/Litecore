// 演示模式：网关连不上时的保底。数据和门控规则照搬旧前端，标注为排练数据。

import type { RegisterResult, Scenario, UEEntry } from './api';

export const DEMO_CHANNEL: Record<Scenario, { signal: number; sinr: number }> = {
  stable: { signal: -82.6, sinr: 18.4 },
  edge: { signal: -108.4, sinr: 1.8 },
  degrading: { signal: -116.2, sinr: -1.2 },
  mixed: { signal: -103.1, sinr: 3.4 },
};

export function demoRegister(ueId: string, scenario: Scenario, sequence: number): RegisterResult {
  const { signal, sinr } = DEMO_CHANNEL[scenario];
  const accepted = signal >= -110 && sinr >= 0;
  const ip = `10.0.${Math.floor((sequence - 1) / 254) % 256}.${((sequence - 1) % 254) + 1}`;
  return {
    success: accepted,
    ueId,
    signalPower: signal,
    sinr,
    retries: 0,
    processingMs: 0,
    gatewayMs: 0,
    message: accepted
      ? '注册和会话建立成功'
      : `信道质量不足(signal=${signal.toFixed(2)} dBm, SINR=${sinr.toFixed(2)} dB)`,
    ...(accepted ? { amfUeId: `AMF-${ueId}`, sessionId: `SESSION-${ueId}`, ueIp: ip } : {}),
  };
}

export function demoUEs(): UEEntry[] {
  return [
    {
      ueId: 'UE-DEMO-01',
      amfUeId: 'AMF-UE-DEMO-01',
      sessionId: 'SESSION-UE-DEMO-01',
      ueIp: '10.0.0.1',
      amfState: 'REGISTERED',
      sessionState: 'ACTIVE',
      ruleId: 'RULE-SESSION-UE-DEMO-01',
      packetsForwarded: 1240,
      active: true,
    },
    {
      ueId: 'UE-DEMO-02',
      amfUeId: 'AMF-UE-DEMO-02',
      sessionId: 'SESSION-UE-DEMO-02',
      ueIp: '10.0.0.2',
      amfState: 'REGISTERED',
      sessionState: 'ACTIVE',
      ruleId: 'RULE-SESSION-UE-DEMO-02',
      packetsForwarded: 640,
      active: true,
    },
  ];
}
