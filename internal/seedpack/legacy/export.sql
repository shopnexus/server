-- The export schema seedpack build legacy reads. Run on a copy of the old
-- ShopNexus dev database (branch escrow), never on the original.
drop schema if exists export cascade; create schema export;
create table export.category as select c.id, c.parent_id, c.name from catalog.category c;
create table export.listing as
select l.id, l.account_id, l.category_id, l.status::text status, l.name, l.description, l.specifications, l.condition::text condition,
       l.created_at, st_y(l.location::geometry) lat, st_x(l.location::geometry) lng, l.ward_code, l.ward_name, l.province_name,
       (select v.price from catalog.variant v where v.listing_id=l.id and v.deleted_at is null order by v.is_featured desc, v.price limit 1) price,
       array(select r.object_key from unnest(l.attachments) with ordinality a(rid,n) join catalog.resource r on r.id=a.rid where r.provider='local' and r.mime in ('image/jpeg','image/png') order by a.n) photos
from catalog.listing l where l.status in ('active','hidden') and l.deleted_at is null;
delete from export.listing where cardinality(photos)=0;
create table export.account as select a.id, a.name, a.description from catalog.listing l join account.account a on a.id=l.account_id group by a.id;
insert into export.account select a.id, a.name, a.description from account.account a where a.id not in (select id from export.account) and a.role='user';
create table export.text_vector as select e.listing_id, e.dense from catalog.listing_embedding e join export.listing l on l.id=e.listing_id where e.dense is not null;
select (select count(*) from export.listing) listings, (select sum(cardinality(photos)) from export.listing) photos, (select count(*) from export.account) accounts, (select count(*) from export.text_vector) vectors, (select count(*) from export.listing where price is null) noprice;
