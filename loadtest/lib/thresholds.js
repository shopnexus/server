import { config } from './config.js';

// The targets a run must meet; a run that misses any of them exits non-zero.
// A journey's mix of routes hides one slow route, so the routes people wait
// on most have targets of their own. A stress run stops a minute after it
// first misses one: the level it reached is the answer it was after.
const targets = {
  http_req_failed: ['rate<0.01'],
  checks: ['rate>0.99'],
  'http_req_duration{journey:guest}': ['p(95)<300', 'p(99)<1000'],
  'http_req_duration{journey:member}': ['p(95)<500', 'p(99)<1500'],
  'http_req_duration{journey:trade}': ['p(95)<500', 'p(99)<1500'],
  'http_req_duration{journey:auth}': ['p(95)<500'],
  'http_req_duration{name:/v1/search}': ['p(95)<500', 'p(99)<1500'],
  'http_req_duration{name:/v1/screens/home}': ['p(95)<300'],
  'http_req_duration{name:/v1/screens/listing/{id}}': ['p(95)<200'],
  'http_req_duration{name:/v1/conversations/{id}/messages}': ['p(95)<300'],
  'http_req_duration{name:/v1/deals/{id}/offer}': ['p(95)<500'],
  socket_ready: ['p(95)<1000'],
  socket_dropped: ['rate<0.01'],
  deal_delivery: ['p(95)<1000'],
  message_delivery: ['p(95)<1000'],
  trade_missed: ['rate<0.01'],
  sessions_lost: ['count<1'],
};

const abort = config.profile === 'stress';

export const thresholds = Object.fromEntries(Object.entries(targets).map(([metric, list]) => [
  metric,
  abort ? list.map((threshold) => ({ threshold, abortOnFail: true, delayAbortEval: '1m' })) : list,
]));
