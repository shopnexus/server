-- Spec keys (2026-11-24). A category's specs are stable keys with a name per
-- language, so a client shows them in any language and a translated name
-- cannot break posting. specs holds the Vietnamese name, which a listing
-- stores as the label and embeddings and search read; spec_names holds the
-- other languages, as category_names does for categories.

CREATE TABLE specs (
    key          text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]*$'),
    display_name text NOT NULL CHECK (display_name <> '')
);

CREATE TABLE spec_names (
    key    text NOT NULL REFERENCES specs (key) ON DELETE CASCADE,
    locale text NOT NULL CHECK (locale <> 'vi'),
    name   text NOT NULL CHECK (name <> ''),
    PRIMARY KEY (key, locale)
);

-- One key per label, except where a label means two things: Kích thước is
-- the screen size of a monitor, Môn a sport or a school subject, Loại máy a
-- watch's movement.
INSERT INTO specs (key, display_name) VALUES
('model', 'Dòng máy'),
('storage', 'Dung lượng'),
('color', 'Màu'),
('battery', 'Pin'),
('brand', 'Hãng'),
('connectivity', 'Kết nối'),
('face_size', 'Kích thước mặt'),
('headphone_type', 'Kiểu tai nghe'),
('accessory_type', 'Loại phụ kiện'),
('fits', 'Dùng cho'),
('chip', 'Chip'),
('ram', 'RAM'),
('drive', 'Ổ cứng'),
('cpu', 'CPU'),
('gpu', 'Card đồ họa'),
('screen_size', 'Kích thước'),
('dimensions', 'Kích thước'),
('resolution', 'Độ phân giải'),
('refresh_rate', 'Tần số quét'),
('component_type', 'Loại linh kiện'),
('details', 'Thông số'),
('device_type', 'Loại thiết bị'),
('machine_type', 'Loại máy'),
('movement', 'Loại máy'),
('kind', 'Loại'),
('size', 'Size'),
('material', 'Chất liệu'),
('gender', 'Giới tính'),
('bag_type', 'Loại túi'),
('vehicle_model', 'Dòng xe'),
('registration_year', 'Năm đăng ký'),
('mileage', 'Số km đã đi'),
('vehicle_type', 'Loại xe'),
('frame_size', 'Cỡ khung'),
('manufacture_year', 'Năm sản xuất'),
('transmission', 'Hộp số'),
('part_type', 'Loại phụ tùng'),
('vehicle', 'Dùng cho xe'),
('seats', 'Số chỗ ngồi'),
('chair_type', 'Loại ghế'),
('table_type', 'Loại bàn'),
('light_type', 'Loại đèn'),
('power', 'Công suất'),
('wash_capacity', 'Khối lượng giặt'),
('door_type', 'Kiểu cửa'),
('inverter', 'Inverter'),
('fan_type', 'Loại quạt'),
('equipment_type', 'Loại dụng cụ'),
('age', 'Độ tuổi'),
('sport', 'Môn'),
('subject', 'Môn'),
('weight', 'Trọng lượng'),
('shutter_count', 'Số shot'),
('mount', 'Ngàm'),
('focal_length', 'Tiêu cự'),
('aperture', 'Khẩu độ'),
('book_title', 'Tên sách'),
('author', 'Tác giả'),
('genre', 'Thể loại'),
('series', 'Tên truyện'),
('volumes', 'Số tập'),
('grade', 'Lớp / Kỳ thi'),
('expiry', 'Hạn sử dụng'),
('fragrance', 'Tên nước hoa'),
('volume', 'Dung tích'),
('instrument', 'Loại nhạc cụ'),
('year', 'Năm');

INSERT INTO spec_names (key, locale, name) VALUES
('model', 'en', 'Model'),
('storage', 'en', 'Storage'),
('color', 'en', 'Color'),
('battery', 'en', 'Battery'),
('brand', 'en', 'Brand'),
('connectivity', 'en', 'Connectivity'),
('face_size', 'en', 'Face size'),
('headphone_type', 'en', 'Headphone type'),
('accessory_type', 'en', 'Accessory type'),
('fits', 'en', 'Made for'),
('chip', 'en', 'Chip'),
('ram', 'en', 'RAM'),
('drive', 'en', 'Drive'),
('cpu', 'en', 'CPU'),
('gpu', 'en', 'Graphics card'),
('screen_size', 'en', 'Screen size'),
('dimensions', 'en', 'Dimensions'),
('resolution', 'en', 'Resolution'),
('refresh_rate', 'en', 'Refresh rate'),
('component_type', 'en', 'Component type'),
('details', 'en', 'Details'),
('device_type', 'en', 'Device type'),
('machine_type', 'en', 'Type'),
('movement', 'en', 'Movement'),
('kind', 'en', 'Type'),
('size', 'en', 'Size'),
('material', 'en', 'Material'),
('gender', 'en', 'Gender'),
('bag_type', 'en', 'Bag type'),
('vehicle_model', 'en', 'Model'),
('registration_year', 'en', 'Registration year'),
('mileage', 'en', 'Mileage'),
('vehicle_type', 'en', 'Vehicle type'),
('frame_size', 'en', 'Frame size'),
('manufacture_year', 'en', 'Manufacture year'),
('transmission', 'en', 'Transmission'),
('part_type', 'en', 'Part type'),
('vehicle', 'en', 'Vehicle'),
('seats', 'en', 'Seats'),
('chair_type', 'en', 'Chair type'),
('table_type', 'en', 'Table type'),
('light_type', 'en', 'Light type'),
('power', 'en', 'Power'),
('wash_capacity', 'en', 'Wash capacity'),
('door_type', 'en', 'Door type'),
('inverter', 'en', 'Inverter'),
('fan_type', 'en', 'Fan type'),
('equipment_type', 'en', 'Equipment type'),
('age', 'en', 'Age range'),
('sport', 'en', 'Sport'),
('subject', 'en', 'Subject'),
('weight', 'en', 'Weight'),
('shutter_count', 'en', 'Shutter count'),
('mount', 'en', 'Mount'),
('focal_length', 'en', 'Focal length'),
('aperture', 'en', 'Aperture'),
('book_title', 'en', 'Title'),
('author', 'en', 'Author'),
('genre', 'en', 'Genre'),
('series', 'en', 'Series'),
('volumes', 'en', 'Volumes'),
('grade', 'en', 'Grade / exam'),
('expiry', 'en', 'Expiry date'),
('fragrance', 'en', 'Fragrance'),
('volume', 'en', 'Volume'),
('instrument', 'en', 'Instrument'),
('year', 'en', 'Year');

-- A subcategory's specs as keys, in spec_labels' order. spec_labels stays
-- for pods of the release before, which read it during a deploy.
ALTER TABLE categories ADD COLUMN spec_keys text[] NOT NULL DEFAULT '{}';

UPDATE categories c SET spec_keys = v.keys
FROM (VALUES
    ('phones.iphone',        ARRAY['model', 'storage', 'color', 'battery']),
    ('phones.android',       ARRAY['brand', 'model', 'storage', 'color']),
    ('phones.tablet',        ARRAY['brand', 'model', 'storage', 'connectivity']),
    ('phones.watch',         ARRAY['brand', 'model', 'face_size']),
    ('phones.headphones',    ARRAY['brand', 'model', 'headphone_type']),
    ('phones.accessories',   ARRAY['accessory_type', 'fits']),
    ('laptop.macbook',       ARRAY['model', 'chip', 'ram', 'drive']),
    ('laptop.gaming',        ARRAY['brand', 'cpu', 'gpu', 'ram', 'drive']),
    ('laptop.office',        ARRAY['brand', 'cpu', 'ram', 'drive']),
    ('laptop.monitor',       ARRAY['brand', 'screen_size', 'resolution', 'refresh_rate']),
    ('laptop.parts',         ARRAY['component_type', 'brand', 'details']),
    ('laptop.network',       ARRAY['device_type', 'brand']),
    ('laptop.printers',      ARRAY['machine_type', 'brand']),
    ('fashion.men',          ARRAY['kind', 'size', 'material']),
    ('fashion.women',        ARRAY['kind', 'size', 'material']),
    ('fashion.shoes',        ARRAY['brand', 'size', 'gender']),
    ('fashion.bags',         ARRAY['brand', 'bag_type', 'material']),
    ('fashion.watches',      ARRAY['brand', 'movement', 'gender']),
    ('fashion.jewelry',      ARRAY['kind', 'material']),
    ('fashion.accessories',  ARRAY['kind', 'brand']),
    ('vehicles.motorbike',   ARRAY['brand', 'vehicle_model', 'registration_year', 'mileage']),
    ('vehicles.bicycle',     ARRAY['brand', 'vehicle_type', 'frame_size']),
    ('vehicles.car',         ARRAY['brand', 'vehicle_model', 'manufacture_year', 'mileage', 'transmission']),
    ('vehicles.parts',       ARRAY['part_type', 'vehicle']),
    ('furniture.sofa',       ARRAY['material', 'seats', 'dimensions']),
    ('furniture.chair',      ARRAY['chair_type', 'material']),
    ('furniture.table',      ARRAY['table_type', 'material', 'dimensions']),
    ('furniture.bed',        ARRAY['material', 'dimensions']),
    ('furniture.storage',    ARRAY['kind', 'material', 'dimensions']),
    ('furniture.lighting',   ARRAY['light_type', 'power']),
    ('home.kitchen',         ARRAY['device_type', 'brand']),
    ('home.washer',          ARRAY['brand', 'wash_capacity', 'door_type']),
    ('home.aircon',          ARRAY['brand', 'power', 'inverter']),
    ('home.fan',             ARRAY['brand', 'fan_type']),
    ('home.audio',           ARRAY['brand', 'device_type', 'connectivity']),
    ('home.cleaning',        ARRAY['brand', 'machine_type']),
    ('home.bedding',         ARRAY['kind', 'dimensions', 'material']),
    ('home.decor',           ARRAY['kind', 'material']),
    ('home.garden',          ARRAY['kind']),
    ('home.tools',           ARRAY['equipment_type', 'brand']),
    ('baby.stroller',        ARRAY['brand', 'age']),
    ('baby.toys',            ARRAY['kind', 'age']),
    ('baby.clothes',         ARRAY['kind', 'size', 'gender']),
    ('baby.highchair',       ARRAY['brand', 'material']),
    ('sports.gym',           ARRAY['equipment_type', 'brand']),
    ('sports.racket',        ARRAY['brand', 'sport', 'weight']),
    ('sports.football',      ARRAY['kind', 'brand', 'size']),
    ('sports.camping',       ARRAY['kind', 'brand']),
    ('sports.clothing',      ARRAY['kind', 'brand', 'size']),
    ('camera.body',          ARRAY['brand', 'model', 'shutter_count']),
    ('camera.lens',          ARRAY['brand', 'mount', 'focal_length', 'aperture']),
    ('camera.action',        ARRAY['brand', 'model', 'resolution']),
    ('camera.security',      ARRAY['brand', 'resolution', 'connectivity']),
    ('camera.accessories',   ARRAY['accessory_type', 'fits']),
    ('books.books',          ARRAY['book_title', 'author', 'genre']),
    ('books.comics',         ARRAY['series', 'volumes']),
    ('books.study',          ARRAY['book_title', 'subject', 'grade']),
    ('books.stationery',     ARRAY['kind', 'brand']),
    ('beauty.makeup',        ARRAY['brand', 'kind', 'expiry']),
    ('beauty.skincare',      ARRAY['brand', 'kind', 'expiry']),
    ('beauty.hair',          ARRAY['brand', 'kind', 'expiry']),
    ('beauty.perfume',       ARRAY['brand', 'fragrance', 'volume']),
    ('beauty.devices',       ARRAY['brand', 'machine_type']),
    ('hobby.music',          ARRAY['instrument', 'brand']),
    ('hobby.games',          ARRAY['brand', 'model', 'storage']),
    ('hobby.collectibles',   ARRAY['kind', 'year']),
    ('hobby.crafts',         ARRAY['kind', 'material']),
    ('pets.supplies',        ARRAY['kind', 'fits']),
    ('pets.homes',           ARRAY['kind', 'dimensions', 'fits'])
) AS v (code, keys)
WHERE c.code = v.code;

-- Each listing's spec whose label names one of its category's specs, case
-- aside, gets that spec's key; a second spec naming the same one stays free.
UPDATE listings l SET specs = k.specs
FROM (
    SELECT m.id, jsonb_agg(CASE WHEN m.key IS NOT NULL AND m.nth = 1
                                THEN jsonb_build_object('key', m.key) || m.spec ELSE m.spec END ORDER BY m.i) AS specs
    FROM (
        SELECT x.id, x.spec, x.i, x.key, row_number() OVER (PARTITION BY x.id, x.key ORDER BY x.i) AS nth
        FROM (
            SELECT l2.id, e.spec, e.i,
                   (SELECT s.key FROM categories c
                    CROSS JOIN LATERAL unnest(c.spec_keys) AS ck (key)
                    JOIN specs s ON s.key = ck.key
                    WHERE c.code = l2.category_code
                      AND lower(s.display_name COLLATE pg_unicode_fast) = lower(btrim(e.spec ->> 'label') COLLATE pg_unicode_fast)
                    LIMIT 1) AS key
            FROM listings l2
            CROSS JOIN LATERAL jsonb_array_elements(l2.specs) WITH ORDINALITY AS e (spec, i)
        ) x
    ) m
    GROUP BY m.id
) k
WHERE l.id = k.id AND l.specs IS DISTINCT FROM k.specs;

-- Search matches the specs' labels and values, not their keys, which are
-- English words a query could hit on every listing.
DROP INDEX listings_search;
ALTER TABLE listings DROP COLUMN specs_text;
ALTER TABLE listings ADD COLUMN specs_text text GENERATED ALWAYS AS (
    jsonb_path_query_array(specs, '$[*].label')::text || ' ' || jsonb_path_query_array(specs, '$[*].value')::text
) STORED;
CREATE INDEX listings_search ON listings USING paradedb (
    id, title, description, specs_text,
    (title::pdb.unicode_words('alias=title_folded', 'ascii_folding=true')),
    (description::pdb.unicode_words('alias=description_folded', 'ascii_folding=true')),
    (specs_text::pdb.unicode_words('alias=specs_folded', 'ascii_folding=true')),
    (category_code::pdb.literal), (category_root::pdb.literal), price, (condition::pdb.literal),
    allow_offers, (status::pdb.literal), published_at, lat, lng
);
