/**
 * Truncate a file path to fit within maxLength by replacing middle segments with "...".
 * Preserves the beginning (drive letter / root directory) and end (filename) of the path.
 *
 * @param {string} path - The file path to truncate
 * @param {number} [maxLength=60] - Maximum allowed length for the result
 * @returns {string} The truncated path, or the original if it fits within maxLength
 */
export function truncatePath(path, maxLength = 60) {
  if (!path || path.length <= maxLength) {
    return path;
  }

  const separator = path.includes('/') ? '/' : '\\';
  const parts = path.split(separator);

  // Need at least 2 parts to truncate meaningfully (a head and a tail)
  if (parts.length < 2) {
    // Single segment, just hard-truncate
    return path.slice(0, maxLength - 3) + '...';
  }

  const head = parts[0]; // Drive letter or root prefix
  const tail = parts[parts.length - 1]; // Filename

  const ellipsis = '...';
  const minimal = head + separator + ellipsis + separator + tail;

  // If even the minimal form exceeds maxLength, hard-truncate the tail
  if (minimal.length > maxLength) {
    const availableForTail = maxLength - head.length - separator.length - ellipsis.length - separator.length;
    if (availableForTail > 0) {
      return head + separator + ellipsis + separator + tail.slice(-availableForTail);
    }
    // Extreme case: just hard-truncate
    return path.slice(0, maxLength - 3) + '...';
  }

  // Try to include as many leading path segments as possible
  let result = minimal;
  for (let i = 1; i < parts.length - 1; i++) {
    const candidate = parts.slice(0, i + 1).join(separator) + separator + ellipsis + separator + tail;
    if (candidate.length <= maxLength) {
      result = candidate;
    } else {
      break;
    }
  }

  return result;
}
