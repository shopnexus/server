import { body } from '../lib/api.js';
import { communes, conditions, priceRanges, provinces, queryText } from '../lib/queries.js';
import { chance, pick, think } from '../lib/think.js';

// Steps both guests and members take.

// The sorts people pick, with their shares. Every sort but relevance reads
// each list at full depth, the costliest search.
const sorts = [['relevance', 0.55], ['newest', 0.15], ['price_asc', 0.1], ['price_desc', 0.05], ['nearest', 0.15]];

function weighted(options) {
  let r = Math.random();
  for (const [value, share] of options) {
    if ((r -= share) < 0) return value;
  }
  return options[0][0];
}

// filters adds what a person narrows a search by, each now and then.
function filters(query) {
  const sort = weighted(sorts);
  if (sort !== 'relevance') query.sort = sort;
  if (sort === 'nearest' || chance(0.1)) {
    query.near_commune = pick(communes);
    if (chance(0.5)) query.radius_km = pick([5, 10, 20, 50]);
  }
  if (chance(0.25)) query.province = pick(provinces);
  if (chance(0.15)) {
    const [min, max] = pick(priceRanges);
    if (min) query.price_min = min;
    if (max) query.price_max = max;
  }
  if (chance(0.1)) query.condition = pick(conditions);
  if (chance(0.05)) query.offers_only = true;
  return query;
}

// search runs one search, a guest's often in words, and reads more pages
// now and then, up to five. A member never searches in words: the model
// reads a signed-in person's words, a paid call, so a member browses a
// category. It returns the listings the last page showed as { id, seller }.
export function search(api, catalog, { words }) {
  const query = { limit: 20 };
  if (words && (chance(0.75) || catalog.categories.length === 0)) {
    query.q = queryText(catalog.queries);
    if (chance(0.15) && catalog.categories.length) query.category = pick(catalog.categories);
  } else {
    query.category = pick(catalog.categories);
  }
  filters(query);
  let page = body(api.get('/v1/search', { query, read: true }));
  let found = page?.items ?? [];
  for (let n = 1; n < 5 && page?.next_cursor && chance(0.35); n++) {
    think(2, 5);
    page = body(api.get('/v1/search', { query: { ...query, cursor: page.next_cursor }, read: true }));
    found = page?.items ?? found;
  }
  return found.map((item) => ({ id: item.id, seller: item.seller_id }));
}

// openListing opens one of the listings a search showed, or any known one
// when it showed none, and returns it. Half the time the person scrolls to
// the listings like it, a hybrid search on the listing's stored vectors.
export function openListing(api, catalog, found) {
  const listing = pick(found) ?? pick(catalog.listings);
  api.get('/v1/screens/listing/{id}', { path: { id: listing.id } });
  if (chance(0.5)) {
    think(2, 5);
    api.get('/v1/search', { query: { like: listing.id, limit: 20 } });
  }
  return listing;
}
