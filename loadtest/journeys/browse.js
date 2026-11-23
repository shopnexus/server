import { body } from '../lib/api.js';
import { chance, pick, think } from '../lib/think.js';

// Steps both guests and members take.

// search types a query or opens a category, sometimes reads the next page,
// and returns the listings it showed as { id, seller }.
export function search(api, catalog) {
  const query = chance(0.7) || catalog.categories.length === 0
    ? { q: pick(catalog.queries), limit: 20 }
    : { category: pick(catalog.categories), limit: 20 };
  let page = body(api.get('/v1/search', { query, read: true }));
  if (page?.next_cursor && chance(0.2)) {
    think(2, 5);
    page = body(api.get('/v1/search', { query: { ...query, cursor: page.next_cursor }, read: true })) ?? page;
  }
  return (page?.items ?? []).map((item) => ({ id: item.id, seller: item.seller_id }));
}

// openListing opens one of the listings a search showed, or any known one
// when it showed none, and returns it.
export function openListing(api, catalog, found) {
  const listing = pick(found) ?? pick(catalog.listings);
  api.get('/v1/screens/listing/{id}', { path: { id: listing.id } });
  return listing;
}
