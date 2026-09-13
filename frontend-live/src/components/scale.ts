// 图表共用的小工具：把数据换算成 SVG 坐标。
export type Range = { min: number; max: number };

// 线性映射：把 value 从 from 区间平移到 to 区间。
export function lin(value: number, from: Range, to: Range): number {
  if (from.max === from.min) return to.min;
  return to.min + ((value - from.min) / (from.max - from.min)) * (to.max - to.min);
}

// 取一个"好看"的坐标上限（1 / 2 / 5 × 10^n），避免 Y 轴顶格数字太怪。
export function niceCeil(value: number): number {
  if (value <= 0) return 1;
  const power = 10 ** Math.floor(Math.log10(value));
  const normalized = value / power;
  const nice = normalized <= 1 ? 1 : normalized <= 2 ? 2 : normalized <= 5 ? 5 : 10;
  return nice * power;
}

// log 刻度：把耗时映射到 log10 空间；0 和负值钳到 1ms，保证不取对数出错。
export function logScale(value: number, from: Range, to: Range): number {
  const clamped = Math.max(value, 1);
  return lin(
    Math.log10(clamped),
    { min: Math.log10(Math.max(from.min, 1)), max: Math.log10(Math.max(from.max, 1)) },
    to,
  );
}
