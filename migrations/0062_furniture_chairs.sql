-- Nội thất gains Ghế: office, dining and folding chairs went to Khác.
INSERT INTO categories (code, parent_code, display_name, sort_order, spec_labels) VALUES
('furniture.chair', 'furniture', 'Ghế', 1, ARRAY['Loại ghế', 'Chất liệu']);

UPDATE categories SET sort_order = sort_order + 1
WHERE parent_code = 'furniture' AND code <> 'furniture.chair' AND code <> 'furniture.sofa';
