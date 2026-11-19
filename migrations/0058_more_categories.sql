-- More of what people resell: cameras, books, beauty, hobbies and pet
-- supplies get roots, and the roots that sent too much to Khác gain leaves.
-- Names that read the same in two places, or no longer cover their leaves,
-- change. Khác stays the last root.

INSERT INTO categories (code, parent_code, display_name, sort_order) VALUES
('camera', NULL, 'Máy ảnh', 8),
('books', NULL, 'Sách & văn phòng phẩm', 9),
('beauty', NULL, 'Sắc đẹp', 10),
('hobby', NULL, 'Sở thích', 11),
('pets', NULL, 'Thú cưng', 12),
('camera.body', 'camera', 'Máy ảnh, máy quay', 0),
('camera.lens', 'camera', 'Ống kính', 1),
('camera.action', 'camera', 'Camera hành trình', 2),
('camera.security', 'camera', 'Camera an ninh', 3),
('camera.accessories', 'camera', 'Phụ kiện máy ảnh', 4),
('camera.other', 'camera', 'Khác', 5),
('books.books', 'books', 'Sách', 0),
('books.comics', 'books', 'Truyện tranh', 1),
('books.study', 'books', 'Sách học, luyện thi', 2),
('books.stationery', 'books', 'Văn phòng phẩm', 3),
('books.other', 'books', 'Khác', 4),
('beauty.makeup', 'beauty', 'Trang điểm', 0),
('beauty.skincare', 'beauty', 'Chăm sóc da', 1),
('beauty.hair', 'beauty', 'Chăm sóc tóc', 2),
('beauty.perfume', 'beauty', 'Nước hoa', 3),
('beauty.devices', 'beauty', 'Máy làm đẹp, sức khỏe', 4),
('beauty.other', 'beauty', 'Khác', 5),
('hobby.music', 'hobby', 'Nhạc cụ', 0),
('hobby.games', 'hobby', 'Máy chơi game', 1),
('hobby.collectibles', 'hobby', 'Đồ sưu tầm', 2),
('hobby.crafts', 'hobby', 'Mỹ thuật, thủ công', 3),
('hobby.other', 'hobby', 'Khác', 4),
('pets.supplies', 'pets', 'Đồ dùng thú cưng', 0),
('pets.homes', 'pets', 'Chuồng, nệm, lồng', 1),
('pets.other', 'pets', 'Khác', 2),
('laptop.network', 'laptop', 'Thiết bị mạng', 5),
('laptop.printers', 'laptop', 'Máy in, máy chiếu', 6),
('fashion.jewelry', 'fashion', 'Trang sức', 5),
('fashion.accessories', 'fashion', 'Kính, mũ, thắt lưng', 6),
('home.audio', 'home', 'Loa, âm thanh', 4),
('home.cleaning', 'home', 'Hút bụi, lọc khí', 5),
('home.bedding', 'home', 'Chăn ga, gối nệm', 6),
('home.decor', 'home', 'Trang trí', 7),
('home.garden', 'home', 'Sân vườn', 8),
('home.tools', 'home', 'Dụng cụ, điện nước', 9),
('sports.clothing', 'sports', 'Quần áo, giày thể thao', 4);

-- Laptop holds monitors, parts and network gear too; two leaves read
-- Đồng hồ, and Văn phòng alone does not say laptop.
UPDATE categories SET display_name = 'Máy tính' WHERE code = 'laptop';
UPDATE categories SET display_name = 'Laptop văn phòng' WHERE code = 'laptop.office';
UPDATE categories SET display_name = 'Đồng hồ thông minh' WHERE code = 'phones.watch';
UPDATE categories SET display_name = 'Phụ kiện điện thoại' WHERE code = 'phones.accessories';

UPDATE categories SET sort_order = 13 WHERE code = 'other';
UPDATE categories SET sort_order = 7 WHERE code IN ('laptop.other', 'fashion.other');
UPDATE categories SET sort_order = 10 WHERE code = 'home.other';
UPDATE categories SET sort_order = 5 WHERE code = 'sports.other';
