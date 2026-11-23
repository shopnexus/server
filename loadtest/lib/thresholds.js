// The targets a run must meet; a run that misses any of them exits non-zero.
export const thresholds = {
  http_req_failed: ['rate<0.01'],
  checks: ['rate>0.99'],
  'http_req_duration{journey:guest}': ['p(95)<300', 'p(99)<1000'],
  'http_req_duration{journey:member}': ['p(95)<500', 'p(99)<1500'],
  'http_req_duration{journey:auth}': ['p(95)<500'],
  socket_ready: ['p(95)<1000'],
  socket_dropped: ['rate<0.01'],
  sessions_lost: ['count<1'],
};
