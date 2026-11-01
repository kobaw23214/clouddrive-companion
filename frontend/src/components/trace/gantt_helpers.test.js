import { describe, it, expect } from 'vitest';
import {
  escapeHtml,
  hexToRgba,
  computeTimeRange,
  buildSeriesPoint,
} from './gantt_helpers.js';

describe('escapeHtml', () => {
  it('escapes basic HTML chars', () => {
    expect(escapeHtml('<script>alert("x")</script>')).toBe(
      '&lt;script&gt;alert(&quot;x&quot;)&lt;/script&gt;'
    );
  });

  it('escapes & to &amp; first', () => {
    expect(escapeHtml('a & b < c')).toBe('a &amp; b &lt; c');
  });

  it('handles null/undefined', () => {
    expect(escapeHtml(null)).toBe('');
    expect(escapeHtml(undefined)).toBe('');
  });

  it('coerces non-strings', () => {
    expect(escapeHtml(123)).toBe('123');
  });
});

describe('hexToRgba', () => {
  it('converts 6-digit hex', () => {
    expect(hexToRgba('#2563eb', 0.12)).toBe('rgba(37,99,235,0.12)');
  });

  it('expands 3-digit hex', () => {
    expect(hexToRgba('#abc', 0.5)).toBe('rgba(170,187,204,0.5)');
  });

  it('handles uppercase hex', () => {
    expect(hexToRgba('#FF00FF', 1)).toBe('rgba(255,0,255,1)');
  });

  it('returns input unchanged for non-hex', () => {
    expect(hexToRgba('rgb(1,2,3)', 0.5)).toBe('rgb(1,2,3)');
  });

  it('falls back to default blue for invalid input', () => {
    expect(hexToRgba('', 0.3)).toBe('rgba(37, 99, 235, 0.3)');
    expect(hexToRgba('#zz', 0.3)).toBe('rgba(37, 99, 235, 0.3)');
    expect(hexToRgba(null, 0.3)).toBe('rgba(37, 99, 235, 0.3)');
  });

  it('rejects 4/5/8-digit hex (invalid)', () => {
    expect(hexToRgba('#1234', 0.1)).toBe('rgba(37, 99, 235, 0.1)');
  });
});

describe('computeTimeRange', () => {
  it('handles empty spans, no parentWindow', () => {
    const r = computeTimeRange([], null, 1000);
    expect(r.min).toBe(1000);
    expect(r.max).toBe(1000);
    expect(r.pad).toBeGreaterThan(0);
  });

  it('uses min/max from spans', () => {
    const spans = [
      { startMs: 100, endMs: 200 },
      { startMs: 50,  endMs: 80 },
      { startMs: 150, endMs: 300 },
    ];
    const r = computeTimeRange(spans, null, 9999);
    expect(r.min).toBe(50);
    expect(r.max).toBe(300);
    expect(r.pad).toBeCloseTo((300 - 50) * 0.05);
  });

  it('extends range with parentWindow', () => {
    const spans = [{ startMs: 100, endMs: 200 }];
    const r = computeTimeRange(spans, { startMs: 0, endMs: 500 }, 9999);
    expect(r.min).toBe(0);
    expect(r.max).toBe(500);
  });

  it('falls back to now when no positive endMs', () => {
    const spans = [{ startMs: 100, endMs: 0 }];
    const r = computeTimeRange(spans, null, 1500);
    expect(r.min).toBe(100);
    expect(r.max).toBe(1500);
  });

  it('ignores non-finite startMs but keeps endMs', () => {
    const spans = [
      { startMs: NaN, endMs: 200 },   // 无 start，仅 end 进入
      { startMs: 100, endMs: 250 },   // 完整 span
    ];
    const r = computeTimeRange(spans, null, 9999);
    expect(r.min).toBe(100);
    expect(r.max).toBe(250);
  });

  it('treats missing endMs (undefined) as in-progress and extends to now', () => {
    const spans = [{ startMs: 100, endMs: undefined }];
    const r = computeTimeRange(spans, null, 5000);
    expect(r.min).toBe(100);
    expect(r.max).toBe(5000);
  });
});

describe('buildSeriesPoint', () => {
  it('builds value array in fixed order', () => {
    const span = {
      spanId: 'abc',
      stage: 'md5_calc',
      startMs: 100,
      endMs: 250,
      durationMs: 150,
      success: true,
    };
    const p = buildSeriesPoint(span, 3, 'md5', 9999);
    expect(p.value[0]).toBe(3);     // yIdx
    expect(p.value[1]).toBe(100);   // start
    expect(p.value[2]).toBe(250);   // end
    expect(p.value[3]).toBe(150);   // duration
    expect(p.value[4]).toBe('md5_calc');
    expect(p.value[5]).toBe('md5');
    expect(p.value[6]).toBe('abc');
    expect(p.value[7]).toBe(1);     // success
    expect(p.value[8]).toBe(0);     // in-progress
    expect(p.span).toBe(span);
  });

  it('marks in-progress when endMs is 0', () => {
    const span = { spanId: 'p', stage: 's', startMs: 100, endMs: 0 };
    const p = buildSeriesPoint(span, 0, '', 999);
    expect(p.value[2]).toBe(999);   // end ← now
    expect(p.value[8]).toBe(1);     // in-progress
  });

  it('marks failure when success === false', () => {
    const span = { spanId: 'f', stage: 's', startMs: 0, endMs: 10, success: false };
    const p = buildSeriesPoint(span, 0, '', 100);
    expect(p.value[7]).toBe(0);
  });

  it('treats missing success as success=true (1)', () => {
    const span = { spanId: 'x', stage: 's', startMs: 0, endMs: 10 };
    const p = buildSeriesPoint(span, 0, '', 100);
    expect(p.value[7]).toBe(1);
  });

  it('falls back name to span-{index} when spanId missing', () => {
    const span = { stage: 's', startMs: 0, endMs: 10 };
    const p = buildSeriesPoint(span, 7, '', 100);
    expect(p.name).toBe('span-7');
  });

  it('computes duration when not provided', () => {
    const span = { spanId: 'd', stage: 's', startMs: 100, endMs: 250 };
    const p = buildSeriesPoint(span, 0, '', 999);
    expect(p.value[3]).toBe(150);
  });
});
