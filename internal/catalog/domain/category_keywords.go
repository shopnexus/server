package domain

// CategoryKeywords are the words sellers use for each subcategory: kinds,
// brands and models, in Vietnamese. GuessCategory compares a listing's text
// with each. The ".other" leaves have none: no text is guessed into Khác.
var CategoryKeywords = map[string][]string{
	"baby.clothes":   {"quần áo trẻ em", "đồ sơ sinh", "quần áo bé trai bé gái"},
	"baby.highchair": {"ghế ăn dặm cho bé", "ghế ngồi ô tô trẻ em"},
	"baby.stroller":  {"xe đẩy em bé", "xe đẩy gấp gọn", "nôi", "địu"},
	"baby.toys":      {"đồ chơi trẻ em", "Lego", "xếp hình", "gấu bông", "búp bê", "xe đồ chơi"},

	"beauty.devices":  {"máy massage", "súng massage", "máy rửa mặt", "máy đo huyết áp", "cân sức khoẻ"},
	"beauty.hair":     {"máy sấy tóc", "máy duỗi tóc", "máy uốn tóc", "dầu gội"},
	"beauty.makeup":   {"son môi", "phấn", "kem nền", "mascara", "bảng màu mắt", "mỹ phẩm trang điểm"},
	"beauty.perfume":  {"nước hoa Chanel", "Dior", "nước hoa nam nữ"},
	"beauty.skincare": {"kem dưỡng da", "serum", "sữa rửa mặt", "kem chống nắng", "toner"},

	"books.books":      {"sách", "tiểu thuyết", "sách kỹ năng", "sách kinh doanh", "combo sách"},
	"books.comics":     {"truyện tranh", "manga", "Doraemon", "Conan", "One Piece"},
	"books.stationery": {"văn phòng phẩm", "bút", "vở", "sổ tay", "máy tính cầm tay"},
	"books.study":      {"sách giáo khoa", "sách luyện thi IELTS", "TOEIC", "đại học"},

	"camera.accessories": {"chân máy tripod", "túi máy ảnh", "pin máy ảnh", "thẻ nhớ", "gimbal"},
	"camera.action":      {"camera hành trình GoPro", "DJI Osmo Action", "Insta360", "camera hành trình ô tô"},
	"camera.body":        {"máy ảnh Sony A7", "Canon", "Fujifilm", "Nikon", "máy ảnh mirrorless", "DSLR", "máy ảnh film", "Polaroid", "máy quay"},
	"camera.lens":        {"ống kính", "lens 50mm f/1.8", "24-70", "lens Sony", "Canon"},
	"camera.security":    {"camera an ninh", "camera wifi Imou", "Ezviz", "camera giám sát"},

	"fashion.accessories": {"kính mát", "mũ lưỡi trai", "nón", "thắt lưng", "khăn choàng"},
	"fashion.bags":        {"túi xách", "balo", "ví", "túi da", "túi đeo chéo", "Charles & Keith"},
	"fashion.jewelry":     {"trang sức", "nhẫn", "dây chuyền", "bông tai", "lắc tay", "vàng", "bạc", "PNJ"},
	"fashion.men":         {"quần áo nam", "áo sơ mi nam", "áo khoác", "áo bomber", "quần jean nam", "áo thun", "vest"},
	"fashion.shoes":       {"giày sneaker", "giày Nike", "Adidas", "giày da", "giày cao gót", "dép", "sandal"},
	"fashion.watches":     {"đồng hồ đeo tay cơ", "Casio", "Seiko", "Citizen", "Orient", "đồng hồ dây da", "dây kim loại"},
	"fashion.women":       {"quần áo nữ", "váy", "đầm", "áo kiểu", "chân váy", "áo dài", "đồ nữ"},

	"furniture.bed":      {"giường ngủ", "khung giường gỗ", "giường tầng"},
	"furniture.lighting": {"đèn bàn", "đèn ngủ", "đèn cây", "đèn trần", "đèn chùm"},
	"furniture.sofa":     {"ghế sofa", "sofa da", "sofa vải", "ghế bành", "đi văng"},
	"furniture.storage":  {"tủ quần áo", "kệ sách", "tủ giày", "kệ tivi", "tủ ngăn kéo"},
	"furniture.table":    {"bàn làm việc", "bàn ăn", "bàn gỗ", "bàn trà", "bàn học"},

	"hobby.collectibles": {"đồ sưu tầm", "mô hình", "figure", "tiền xu", "tem"},
	"hobby.crafts":       {"dụng cụ vẽ", "màu vẽ", "cọ", "đồ thủ công", "len đan"},
	"hobby.games":        {"máy chơi game PS5", "PS4", "Nintendo Switch", "Xbox", "tay cầm", "đĩa game"},
	"hobby.music":        {"đàn guitar", "đàn piano", "organ", "ukulele", "trống", "nhạc cụ"},

	"home.aircon":   {"máy lạnh", "điều hoà Daikin", "Panasonic", "inverter 1HP 1.5HP"},
	"home.audio":    {"loa bluetooth", "loa JBL", "Marshall", "dàn âm thanh", "amply", "soundbar", "tivi"},
	"home.bedding":  {"chăn ga gối", "nệm", "đệm cao su", "gối", "chăn"},
	"home.cleaning": {"máy hút bụi", "robot hút bụi", "máy lọc không khí", "Xiaomi", "Dyson"},
	"home.decor":    {"đồ trang trí", "tranh treo tường", "bình hoa", "khung ảnh", "thảm"},
	"home.fan":      {"quạt điện", "quạt đứng", "quạt bàn", "quạt điều hoà", "quạt trần"},
	"home.garden":   {"cây cảnh", "chậu cây", "dụng cụ làm vườn", "sân vườn"},
	"home.kitchen":  {"đồ nhà bếp", "nồi cơm điện", "nồi chiên không dầu", "máy xay sinh tố", "lò vi sóng", "bếp từ", "chảo", "tủ lạnh"},
	"home.tools":    {"dụng cụ", "máy khoan", "bộ tua vít", "máy bắn vít", "đồ điện nước"},
	"home.washer":   {"máy giặt cửa trước", "cửa trên", "máy sấy quần áo", "LG", "Electrolux"},

	"laptop.gaming":   {"laptop gaming Asus ROG", "TUF", "Acer Nitro", "Predator", "MSI", "Legion", "card RTX"},
	"laptop.macbook":  {"MacBook Air", "MacBook Pro M1 M2 M3", "laptop Apple"},
	"laptop.monitor":  {"màn hình máy tính 24 inch 27 inch", "Dell", "LG", "Samsung", "màn hình 144Hz 2K 4K"},
	"laptop.network":  {"router wifi", "bộ phát wifi", "mesh", "switch mạng", "TP-Link", "modem"},
	"laptop.office":   {"laptop văn phòng Dell Latitude", "Inspiron", "HP", "Lenovo ThinkPad", "Asus Vivobook", "máy tính xách tay"},
	"laptop.parts":    {"linh kiện máy tính", "bàn phím cơ", "chuột", "RAM", "SSD", "card đồ hoạ VGA", "CPU", "mainboard", "PC"},
	"laptop.printers": {"máy in Canon", "HP", "Brother", "máy in laser", "máy chiếu", "máy scan"},

	"pets.homes":    {"chuồng chó", "lồng chim", "nệm cho mèo", "nhà cho thú cưng"},
	"pets.supplies": {"đồ dùng thú cưng", "thức ăn cho chó mèo", "dây dắt", "bát ăn", "cát vệ sinh"},

	"phones.accessories": {"ốp lưng", "cáp sạc", "củ sạc nhanh", "sạc dự phòng", "kính cường lực", "giá đỡ điện thoại"},
	"phones.android":     {"điện thoại Samsung Galaxy S", "A", "Z Fold", "Xiaomi Redmi", "Oppo", "Vivo", "Realme", "Google Pixel", "smartphone Android"},
	"phones.headphones":  {"tai nghe AirPods", "tai nghe bluetooth không dây", "Sony WH-1000XM", "tai nghe chụp tai", "earbuds"},
	"phones.iphone":      {"iPhone 11", "12", "13", "14", "15", "16 Pro Max", "điện thoại Apple"},
	"phones.tablet":      {"máy tính bảng iPad Air", "iPad Pro", "iPad mini", "Galaxy Tab", "tablet", "bút Apple Pencil"},
	"phones.watch":       {"đồng hồ thông minh Apple Watch", "Galaxy Watch", "Garmin", "Xiaomi Mi Band", "vòng đeo tay"},

	"sports.camping":  {"lều cắm trại", "đồ cắm trại", "ghế xếp", "túi ngủ", "đèn pin", "dã ngoại"},
	"sports.clothing": {"quần áo thể thao", "giày chạy bộ", "đồ tập"},
	"sports.football": {"bóng đá", "giày đá bóng", "áo đấu", "găng thủ môn"},
	"sports.gym":      {"dụng cụ tập gym", "tạ tay", "tạ đơn", "máy chạy bộ", "xe đạp tập", "thảm yoga"},
	"sports.racket":   {"vợt cầu lông Yonex", "Lining", "vợt tennis", "quả cầu", "pickleball"},

	"vehicles.bicycle":   {"xe đạp thể thao", "xe đạp đường phố", "Giant", "Trek", "fixie", "xe đạp trẻ em", "xe đạp địa hình"},
	"vehicles.car":       {"ô tô", "xe hơi Toyota Vios", "Honda City", "Kia", "Mazda", "Hyundai", "xe 4 chỗ 7 chỗ"},
	"vehicles.motorbike": {"xe máy Honda Vision", "Air Blade", "SH", "Wave", "Yamaha Exciter", "Sirius", "xe tay ga", "xe số", "xe côn tay"},
	"vehicles.parts":     {"phụ tùng xe", "mũ bảo hiểm", "lốp xe", "đồ chơi xe máy", "gương", "phuộc", "nhớt"},
}
