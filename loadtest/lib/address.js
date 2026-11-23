import exec from 'k6/execution';
import { config } from './config.js';

// addressHeaders gives the VU its own client address when SPREAD_ADDRESSES is
// on, so the server's per-address rate limits count each VU as one person.
// 198.18.0.0/15 is the range RFC 2544 sets aside for benchmarks.
export function addressHeaders() {
  if (!config.spreadAddresses) return {};
  const n = exec.vu.idInTest;
  return { 'X-Forwarded-For': `198.${18 + ((n >> 16) & 1)}.${(n >> 8) & 255}.${n & 255}` };
}
