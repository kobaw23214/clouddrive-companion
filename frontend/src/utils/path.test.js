import { describe, it, expect } from 'vitest';
import { truncatePath } from './path.js';

describe('truncatePath', () => {
  it('returns short paths unchanged', () => {
    expect(truncatePath('short.txt', 60)).toBe('short.txt');
  });

  it('returns path unchanged when exactly at maxLength', () => {
    const path = 'a'.repeat(60);
    expect(truncatePath(path, 60)).toBe(path);
  });

  it('returns empty string as-is', () => {
    expect(truncatePath('', 60)).toBe('');
  });

  it('returns null/undefined as-is', () => {
    expect(truncatePath(null, 60)).toBe(null);
    expect(truncatePath(undefined, 60)).toBe(undefined);
  });

  it('truncates Windows paths preserving drive and filename', () => {
    const path = 'D:\\folder1\\folder2\\folder3\\very-long-name\\file.txt';
    const result = truncatePath(path, 40);
    expect(result.length).toBeLessThanOrEqual(40);
    expect(result).toMatch(/^D:\\/);
    expect(result).toMatch(/\\file\.txt$/);
    expect(result).toContain('...');
  });

  it('truncates Unix paths preserving root and filename', () => {
    const path = '/home/user/documents/projects/very-long-dir/file.txt';
    const result = truncatePath(path, 35);
    expect(result.length).toBeLessThanOrEqual(35);
    expect(result).toMatch(/^\//);
    expect(result).toMatch(/\/file\.txt$/);
    expect(result).toContain('...');
  });

  it('includes as many leading segments as possible', () => {
    const path = 'D:\\folder1\\folder2\\folder3\\deeply\\nested\\file.txt';
    const result = truncatePath(path, 35);
    expect(result.length).toBeLessThanOrEqual(35);
    expect(result).toContain('...');
    expect(result.endsWith('\\file.txt')).toBe(true);
    // Should keep D:\folder1 at minimum
    expect(result.startsWith('D:\\')).toBe(true);
  });

  it('uses default maxLength of 60', () => {
    const path = 'D:\\' + 'a\\'.repeat(30) + 'file.txt';
    const result = truncatePath(path);
    expect(result.length).toBeLessThanOrEqual(60);
  });

  it('handles single-segment long names', () => {
    const path = 'a'.repeat(100);
    const result = truncatePath(path, 50);
    expect(result.length).toBeLessThanOrEqual(50);
    expect(result).toContain('...');
  });

  it('handles path with only two segments', () => {
    const path = 'D:\\verylongfilename-that-exceeds-the-limit.txt';
    const result = truncatePath(path, 30);
    expect(result.length).toBeLessThanOrEqual(30);
    expect(result).toContain('...');
  });
});
