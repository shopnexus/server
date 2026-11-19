# vn-catalog

A ShopNexus seed pack: people, listings, sold items with both-way reviews
and follows, built by `seedpack build legacy` from the old dev database.

- Listings and photos come from Tiki product pages that the old dev
  database had scraped. **Internal use only**: do not publish this pack.
- Groceries, medicine, hygiene and other consumables are left out; the old
  store categories are mapped onto ShopNexus's tree.
- People, names, bios, addresses, sales, reviews, replies and follows are
  generated from the builder's CC0 corpus; nobody here is real.
- Text vectors are the old database's BGE-M3 vectors of the old text
  (`origin: legacy`), close to but not exactly the vector of each
  listing's current text. Photo vectors, when present, are SigLIP 2 of the
  processed photos, the same the server computes.
