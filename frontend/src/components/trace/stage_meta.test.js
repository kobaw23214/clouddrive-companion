import { describe, it, expect } from 'vitest';
import {
  STAGES,
  TRACE_TYPES,
  stageLabel,
  stageColor,
  traceTypeLabel,
  formatMs,
  formatTimeShort,
  formatTimeFull,
  formatBytes,
} from './stage_meta.js';

describe('stage_meta — STAGES enum', () => {
  it('exports all 8 stage codes matching backend', () => {
    expect(STAGES.FILE_SCAN).toBe('file_scan');
    expect(STAGES.UPLOAD_TASK).toBe('upload_task');
    expect(STAGES.UPLOAD_FILE).toBe('upload_file');
    expect(STAGES.MD5_CALC).toBe('md5_calc');
    expect(STAGES.CHECK_CHUNKS).toBe('check_chunks');
    expect(STAGES.CHUNK_UPLOAD).toBe('chunk_upload');
    expect(STAGES.NOTIFY_COMPLETE).toBe('notify_complete');
    expect(STAGES.OTHER).toBe('other');
  });

  it('STAGES is frozen (immutable)', () => {
    expect(Object.isFrozen(STAGES)).toBe(true);
  });
});

describe('stageLabel', () => {
  it('returns Chinese label for known stages', () => {
    expect(stageLabel('file_scan')).toBe('目录扫描');
    expect(stageLabel('chunk_upload')).toBe('分片上传');
    expect(stageLabel('md5_calc')).toBe('MD5 计算');
  });

  it('falls back to raw stage for unknown values', () => {
    expect(stageLabel('invented_stage')).toBe('invented_stage');
  });

  it('returns dash for empty/null', () => {
    expect(stageLabel('')).toBe('—');
    expect(stageLabel(null)).toBe('—');
    expect(stageLabel(undefined)).toBe('—');
  });
});

describe('stageColor', () => {
  it('returns css var for known stage', () => {
    expect(stageColor('md5_calc')).toBe('var(--tt-stage-md5)');
  });

  it('falls back to --tt-stage-other for unknown', () => {
    expect(stageColor('xx')).toBe('var(--tt-stage-other)');
  });
});

describe('traceTypeLabel', () => {
  it('returns Chinese label for known types', () => {
    expect(traceTypeLabel(TRACE_TYPES.UPLOAD)).toBe('文件上传');
    expect(traceTypeLabel(TRACE_TYPES.SCAN)).toBe('目录扫描');
  });

  it('falls back to raw type or 未知', () => {
    expect(traceTypeLabel('mystery')).toBe('mystery');
    expect(traceTypeLabel('')).toBe('未知');
  });
});

describe('formatMs', () => {
  it('handles sub-second', () => {
    expect(formatMs(0)).toBe('0 ms');
    expect(formatMs(123)).toBe('123 ms');
    expect(formatMs(999)).toBe('999 ms');
  });

  it('handles seconds', () => {
    expect(formatMs(1000)).toBe('1.0 s');
    expect(formatMs(1500)).toBe('1.5 s');
    expect(formatMs(59_000)).toBe('59.0 s');
  });

  it('handles minutes', () => {
    expect(formatMs(60_000)).toBe('1.0 min');
    expect(formatMs(150_000)).toBe('2.5 min');
  });

  it('handles hours', () => {
    expect(formatMs(3_600_000)).toBe('1.0 h');
    expect(formatMs(7_200_000)).toBe('2.0 h');
  });

  it('returns dash for invalid input', () => {
    expect(formatMs(null)).toBe('—');
    expect(formatMs(undefined)).toBe('—');
    expect(formatMs(-5)).toBe('—');
    expect(formatMs(NaN)).toBe('—');
    expect(formatMs('foo')).toBe('—');
  });
});

describe('formatTimeShort', () => {
  it('returns dash for falsy', () => {
    expect(formatTimeShort(0)).toBe('—');
    expect(formatTimeShort(null)).toBe('—');
  });

  it('formats valid ms timestamp', () => {
    // 2026-05-16 12:00:00 UTC+8
    const ms = new Date('2026-05-16T04:00:00Z').getTime();
    const out = formatTimeShort(ms);
    // 测试只断言含 ":"，避免依赖时区
    expect(out).toMatch(/^\d{2}:\d{2}:\d{2}$/);
  });
});

describe('formatTimeFull', () => {
  it('returns dash for falsy', () => {
    expect(formatTimeFull(0)).toBe('—');
  });

  it('formats valid timestamp', () => {
    const ms = new Date('2026-05-16T04:00:00Z').getTime();
    const out = formatTimeFull(ms);
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
  });
});

describe('formatBytes', () => {
  it('handles ranges', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1024)).toBe('1.0 KB');
    expect(formatBytes(1500)).toBe('1.5 KB');
    expect(formatBytes(2 * 1024 * 1024)).toBe('2.0 MB');
    expect(formatBytes(1.5 * 1024 * 1024 * 1024)).toBe('1.50 GB');
  });

  it('returns dash for invalid', () => {
    expect(formatBytes(null)).toBe('—');
    expect(formatBytes(undefined)).toBe('—');
    expect(formatBytes('xx')).toBe('—');
  });
});
