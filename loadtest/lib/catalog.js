import exec from 'k6/execution';
import { Api, body } from './api.js';

// Search terms people type; open() resolves from this file.
const queries = JSON.parse(open('../data/queries.json'));

// discover runs once, in setup: it finds the categories and the listings the
// journeys visit, so they work on whatever data a stack holds. Its result
// reaches every VU, so it keeps only ids.
export function discover() {
  const api = new Api('setup');
  const categories = (body(api.get('/v1/categories', { read: true }))?.items ?? []).map((c) => c.code);
  const sellers = new Map();
  for (const q of queries) {
    const page = body(api.get('/v1/search', { query: { q, limit: 50 }, read: true }));
    for (const item of page?.items ?? []) sellers.set(item.id, item.seller_id);
  }
  if (sellers.size === 0) exec.test.abort('no search found a listing; is the stack seeded?');
  const listings = [...sellers].map(([id, seller]) => ({ id, seller }));
  return { queries, categories, listings };
}
