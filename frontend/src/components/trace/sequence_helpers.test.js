import { describe, it, expect } from 'vitest';
import {
  LANES,
  lanePair,
  statusKind,
  colorVarFor,
  directionLabel,
  eventEndMs,
  buildSequencePoints,
  computeSequenceTimeRange,
} from './sequence_helpers.js';

describe('LANES', () => {
  it('固定三条泳道，顺序为 浏览器/客户端/远端后端', () => {
    expect(LANES).toEqual(['浏览器', '客户端', '远端后端']);
  });
});

describe('lanePair', () => {
  it("in：浏览器(0) → 客户端(1)", () => {
    expect(lanePair('in')).toEqual({ from: 0, to: 1 });
  });

  it("out：客户端(1) → 远端后端(2)", () => {
    expect(lanePair('out')).toEqual({ from: 1, to: 2 });
  });

  it('未知/缺失 direction 按 in 处理', () => {
    expect(lanePair('')).toEqual({ from: 0, to: 1 });
    expect(lanePair(undefined)).toEqual({ from: 0, to: 1 });
    expect(lanePair('sideways')).toEqual({ from: 0, to: 1 });
  });
});

describe('statusKind', () => {
  it('2xx/3xx 视为 ok', () => {
    expect(statusKind(200)).toBe('ok');
    expect(statusKind(201)).toBe('ok');
    expect(statusKind(301)).toBe('ok');
    expect(statusKind(399)).toBe('ok');
  });

  it('4xx/5xx 视为 fail', () => {
    expect(statusKind(400)).toBe('fail');
    expect(statusKind(404)).toBe('fail');
    expect(statusKind(500)).toBe('fail');
  });

  it('0（网络错误）与非法值视为 fail', () => {
    expect(statusKind(0)).toBe('fail');
    expect(statusKind(NaN)).toBe('fail');
    expect(statusKind(undefined)).toBe('fail');
    expect(statusKind(null)).toBe('fail');
    expect(statusKind(-1)).toBe('fail');
  });
});

describe('colorVarFor', () => {
  it('失败一律红色，不分方向', () => {
    expect(colorVarFor('in', 'fail')).toBe('--tt-seq-fail');
    expect(colorVarFor('out', 'fail')).toBe('--tt-seq-fail');
  });

  it('正常时入站绿、出站蓝', () => {
    expect(colorVarFor('in', 'ok')).toBe('--tt-seq-in');
    expect(colorVarFor('out', 'ok')).toBe('--tt-seq-out');
  });
});

describe('directionLabel', () => {
  it('in/out 各有可读标签', () => {
    expect(directionLabel('in')).toBe('浏览器 → 客户端');
    expect(directionLabel('out')).toBe('客户端 → 远端后端');
  });

  it('未知 direction 按 in 显示', () => {
    expect(directionLabel('')).toBe('浏览器 → 客户端');
  });
});

describe('eventEndMs', () => {
  it('结束时刻 = timestampMs + durationMs', () => {
    expect(eventEndMs({ timestampMs: 1000, durationMs: 250 })).toBe(1250);
  });

  it('durationMs 缺失/为负时退化为 timestampMs', () => {
    expect(eventEndMs({ timestampMs: 1000 })).toBe(1000);
    expect(eventEndMs({ timestampMs: 1000, durationMs: 0 })).toBe(1000);
    expect(eventEndMs({ timestampMs: 1000, durationMs: -5 })).toBe(1000);
  });

  it('timestampMs 非法时返回 NaN', () => {
    expect(Number.isNaN(eventEndMs({}))).toBe(true);
    expect(Number.isNaN(eventEndMs(null))).toBe(true);
    expect(Number.isNaN(eventEndMs({ timestampMs: 'abc' }))).toBe(true);
  });
});

describe('buildSequencePoints', () => {
  it('value 数组按固定顺序填充，并挂载原始 log', () => {
    const log = {
      id: 7,
      timestampMs: 1000,
      durationMs: 120,
      method: 'POST',
      path: '/upload',
      statusCode: 200,
      direction: 'out',
      taskId: 'task-1',
    };
    const [p] = buildSequencePoints([log]);
    expect(p.value[0]).toBe(1);        // fromLane：客户端
    expect(p.value[1]).toBe(2);        // toLane：远端后端
    expect(p.value[2]).toBe(1000);     // startMs
    expect(p.value[3]).toBe(1120);     // endMs
    expect(p.value[4]).toBe(120);      // durationMs
    expect(p.value[5]).toBe('ok');     // kind
    expect(p.value[6]).toBe('out');    // direction
    expect(p.value[7]).toBe('--tt-seq-out');
    expect(p.log).toBe(log);
    expect(p.name).toBe('POST /upload');
  });

  it('入站失败事件：泳道 0→1，kind=fail，红色变量', () => {
    const [p] = buildSequencePoints([
      { timestampMs: 10, durationMs: 5, statusCode: 500, direction: 'in' },
    ]);
    expect(p.value[0]).toBe(0);
    expect(p.value[1]).toBe(1);
    expect(p.value[5]).toBe('fail');
    expect(p.value[7]).toBe('--tt-seq-fail');
  });

  it('跳过没有时间戳的事件', () => {
    const pts = buildSequencePoints([
      { timestampMs: 100, statusCode: 200, direction: 'in' },
      { statusCode: 200, direction: 'in' },               // 无 timestampMs
      { timestampMs: NaN, statusCode: 200 },              // 非法
    ]);
    expect(pts).toHaveLength(1);
  });

  it('空输入返回空数组', () => {
    expect(buildSequencePoints([])).toEqual([]);
    expect(buildSequencePoints(null)).toEqual([]);
    expect(buildSequencePoints(undefined)).toEqual([]);
  });

  it('name 缺失 method/path 时回退为 event-N', () => {
    const [p] = buildSequencePoints([{ timestampMs: 1, statusCode: 200 }]);
    expect(p.name).toBe('event-0');
  });
});

describe('computeSequenceTimeRange', () => {
  it('覆盖所有事件的 [start, end] 区间', () => {
    const r = computeSequenceTimeRange([
      { timestampMs: 100, durationMs: 50 },   // [100, 150]
      { timestampMs: 120, durationMs: 300 },  // [120, 420]
      { timestampMs: 90,  durationMs: 0 },    // [90, 90]
    ]);
    expect(r.min).toBe(90);
    expect(r.max).toBe(420);
    expect(r.pad).toBeCloseTo((420 - 90) * 0.05);
  });

  it('单个零耗时事件也有正 pad', () => {
    const r = computeSequenceTimeRange([{ timestampMs: 1000, durationMs: 0 }]);
    expect(r.min).toBe(1000);
    expect(r.max).toBe(1000);
    expect(r.pad).toBeGreaterThan(0);
  });

  it('无可落图事件时返回 null', () => {
    expect(computeSequenceTimeRange([])).toBeNull();
    expect(computeSequenceTimeRange(null)).toBeNull();
    expect(computeSequenceTimeRange([{ statusCode: 200 }])).toBeNull();
  });
});
