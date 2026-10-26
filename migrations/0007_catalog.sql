-- Catalog: the category tree and the listings. The categories are app2's
-- (core/data fixtures/catalog.json): 8 roots, each with its subcategories
-- and a last "Khác", and a root "Khác" with one subcategory. A listing takes
-- a subcategory. Photos, marks and specs live in the listing row, since they
-- are always read and written with it.

CREATE TABLE categories (
    code         text PRIMARY KEY,
    parent_code  text REFERENCES categories (code),
    display_name text NOT NULL,
    sort_order   integer NOT NULL
);

INSERT INTO categories (code, parent_code, display_name, sort_order) VALUES
('phones', NULL, 'Điện thoại', 0),
('laptop', NULL, 'Laptop', 1),
('fashion', NULL, 'Thời trang', 2),
('vehicles', NULL, 'Xe cộ', 3),
('furniture', NULL, 'Nội thất', 4),
('home', NULL, 'Đồ gia dụng', 5),
('baby', NULL, 'Mẹ & bé', 6),
('sports', NULL, 'Thể thao', 7),
('other', NULL, 'Khác', 8),
('phones.iphone', 'phones', 'iPhone', 0),
('phones.android', 'phones', 'Android', 1),
('phones.tablet', 'phones', 'Máy tính bảng', 2),
('phones.watch', 'phones', 'Đồng hồ', 3),
('phones.headphones', 'phones', 'Tai nghe', 4),
('phones.accessories', 'phones', 'Phụ kiện', 5),
('phones.other', 'phones', 'Khác', 6),
('laptop.macbook', 'laptop', 'MacBook', 0),
('laptop.gaming', 'laptop', 'Laptop gaming', 1),
('laptop.office', 'laptop', 'Văn phòng', 2),
('laptop.monitor', 'laptop', 'Màn hình', 3),
('laptop.parts', 'laptop', 'Linh kiện', 4),
('laptop.other', 'laptop', 'Khác', 5),
('fashion.men', 'fashion', 'Nam', 0),
('fashion.women', 'fashion', 'Nữ', 1),
('fashion.shoes', 'fashion', 'Giày', 2),
('fashion.bags', 'fashion', 'Túi xách', 3),
('fashion.watches', 'fashion', 'Đồng hồ', 4),
('fashion.other', 'fashion', 'Khác', 5),
('vehicles.motorbike', 'vehicles', 'Xe máy', 0),
('vehicles.bicycle', 'vehicles', 'Xe đạp', 1),
('vehicles.car', 'vehicles', 'Ô tô', 2),
('vehicles.parts', 'vehicles', 'Phụ tùng', 3),
('vehicles.other', 'vehicles', 'Khác', 4),
('furniture.sofa', 'furniture', 'Sofa', 0),
('furniture.table', 'furniture', 'Bàn', 1),
('furniture.bed', 'furniture', 'Giường', 2),
('furniture.storage', 'furniture', 'Tủ, kệ', 3),
('furniture.lighting', 'furniture', 'Đèn', 4),
('furniture.other', 'furniture', 'Khác', 5),
('home.kitchen', 'home', 'Nhà bếp', 0),
('home.washer', 'home', 'Máy giặt', 1),
('home.aircon', 'home', 'Điều hòa', 2),
('home.fan', 'home', 'Quạt', 3),
('home.other', 'home', 'Khác', 4),
('baby.stroller', 'baby', 'Xe đẩy', 0),
('baby.toys', 'baby', 'Đồ chơi', 1),
('baby.clothes', 'baby', 'Quần áo bé', 2),
('baby.highchair', 'baby', 'Ghế ăn', 3),
('baby.other', 'baby', 'Khác', 4),
('sports.gym', 'sports', 'Gym', 0),
('sports.racket', 'sports', 'Cầu lông, tennis', 1),
('sports.football', 'sports', 'Bóng đá', 2),
('sports.camping', 'sports', 'Cắm trại', 3),
('sports.other', 'sports', 'Khác', 4),
('other.other', 'other', 'Khác', 0);

CREATE TABLE listings (
    id            uuid PRIMARY KEY,
    seller_id     uuid NOT NULL REFERENCES accounts (id),
    title         text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 70),
    price         bigint NOT NULL CHECK (price >= 10000),
    description   text NOT NULL CHECK (char_length(description) <= 1500),
    category_code text NOT NULL REFERENCES categories (code),
    condition     text NOT NULL CHECK (condition IN ('new', 'like_new', 'used')),
    allow_offers  boolean NOT NULL,
    commune_code  text NOT NULL REFERENCES communes (code),
    photos        jsonb NOT NULL,
    marks         jsonb NOT NULL,
    specs         jsonb NOT NULL,
    status        text NOT NULL
    CHECK (status IN ('processing', 'active', 'hidden', 'rejected', 'sold', 'deleted')),
    -- Per upload, why processing refused it; set while rejected.
    rejection     jsonb,
    write_id      uuid NOT NULL,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL,
    published_at  timestamptz,
    sold_at       timestamptz,
    deleted_at    timestamptz
);

CREATE INDEX listings_seller ON listings (seller_id, status, id);
