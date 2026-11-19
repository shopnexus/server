// Package legacy turns the old ShopNexus dev database into a seed pack. The
// old catalog came from Tiki product pages, so it maps a store's categories
// onto ours and drops what nobody resells.
package legacy

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// homeOther takes what no other leaf of Đồ gia dụng fits.
const homeOther = "home.other"

// rule sends a title that matches to a leaf.
type rule struct {
	leaf string
	re   *regexp.Regexp
}

// route is what one old leaf category becomes: the first rule that matches
// the title, else the fallback. An empty fallback drops the listing.
type route struct {
	rules    []rule
	fallback string
}

func r(leaf, pattern string) rule { return rule{leaf, regexp.MustCompile(`(?i)` + pattern)} }

// consumable is what a person does not resell second-hand: food, medicine,
// hygiene and farming supplies. A match drops the listing in any category.
var consumable = regexp.MustCompile(`(?i)viên uống|tpbvsk|thực phẩm|sữa bột|sữa non|sữa tươi|sữa hạt|(^|\s)tã(\s|$)|bỉm|băng vệ sinh|bao cao su|gel bôi trơn|` +
	`khẩu trang|phân bón|hạt giống|thuốc|kem đánh răng|nước súc miệng|mật ong|(^|\s)trà (xanh|túi|thảo mộc|sữa)|cà phê (hạt|bột|hòa tan)|bánh (quy|ngọt|mì|snack|tráng)|kẹo|nước mắm|gia vị|` +
	`dầu gội|sữa tắm|nước giặt|nước rửa|giấy vệ sinh|khăn giấy|khăn ướt|mực in|hộp mực|ống mực|vitamin|collagen|men vi sinh|` +
	`dung dịch vệ sinh|thức ăn|pate|cát vệ sinh|que thử|phụ gia nhiên liệu|nhớt|dầu nhớt|keo dán|tinh dầu|` +
	`phiếu quà|e-voucher|giftpop|nước tẩy|bơ đậu phộng|ngũ cốc|xà phòng|xà bông|dầu xả|gel rửa|sữa rửa|tăm bông|` +
	`lăn khử|xịt khử|khử mùi|nhang|nụ trầm|trầm hương|sữa công thức|bột ăn|bánh ăn dặm|bàn chải đánh răng`)

var routes = map[string]route{
	// Electronics.
	"Điện thoại & Máy tính bảng": {[]rule{
		r("phones.iphone", `iphone`), r("phones.tablet", `ipad|máy tính bảng|tablet|tab s|kindle|máy đọc sách`),
		r("phones.accessories", `ốp|cường lực|sạc|cáp|dán|(^|\s)sim(\s|$)|giá đỡ`),
	}, "phones.android"},
	"Âm thanh & Tai nghe": {[]rule{
		r("phones.accessories", `^(cáp|dây|đầu|bộ chuyển|chuyển đổi)|kính cường lực`),
		r("phones.headphones", `tai nghe|airpods|earbud|headphone|đệm tai`), r("home.audio", `loa|micro|karaoke|amply|ampli|soundbar|âm ly`),
	}, "home.audio"},
	"Máy ảnh & Quay phim": {[]rule{
		r("camera.security", `camera (giám sát|quan sát|ip|wifi|wi-fi|an ninh|trong nhà|ngoài trời|thông minh|pt|hồng ngoại|hd)|`+
			`hikvision|ezviz|imou|tapo|kbvision|dahua|vantech|yoosee|đầu ghi`),
		r("vehicles.parts", `camera (lùi|ô ?tô|hành trình ô tô)|camera ôtô`),
		r("camera.action", `(camera|cam) (hành|hàng) trình|gopro|action cam|dashcam|ulanzi`),
		r("camera.other", `ống nhòm|kính hiển vi|realsense|astra|camera 3d|máy bẫy`),
		r("camera.accessories", `chân máy|chân đế|tripod|túi|thẻ nhớ|(^|\s)pin|sạc|gimbal|đèn|filter|kính lọc|dây|balo|`+
			`hút ẩm|micro|(^|\s)mic(\s|$)|cho máy ảnh|bộ kit`),
		r("camera.lens", `ống kính|(^|\s)lens(\s|$)`),
		r("camera.body", `^(máy ảnh|máy quay|máy chụp)|instax|polaroid|fujifilm|canon eos|nikon|lumix|sony (a|zv)`),
	}, "camera.other"},
	"Máy tính & Thiết bị mạng": {[]rule{
		r("laptop.parts", `ổ cứng|ssd|hdd|(^|\s)ram(\s|$)|cpu|processor|mainboard|bo mạch|card màn hình|vga|nguồn máy tính|tản nhiệt|(^|\s)(pin|sạc|adapter)\s|bàn phím|chuột`),
		r("laptop.macbook", `macbook`), r("laptop.monitor", `màn hình|monitor`),
		r("laptop.gaming", `laptop gaming|gaming laptop|(^|\s)rog\s|legion|predator|nitro`),
		r("laptop.office", `^laptop|máy tính xách tay|thinkpad|vivobook|ideapad|inspiron|latitude|elitebook`),
		r("laptop.parts", `ổ cứng|ssd|hdd|(^|\s)ram(\s|$)|cpu|processor|mainboard|bo mạch|card màn hình|vga|nguồn máy tính|tản nhiệt|pin.*laptop|sạc.*laptop|bàn phím|chuột`),
		r("laptop.network", network),
		r("laptop.printers", `máy in|máy chiếu|máy quét|máy scan|nhãn in|máy chấm công|máy hủy|mực in|màn chiếu`),
	}, "laptop.other"},
	"Phụ kiện điện tử": {[]rule{
		r("phones.watch", `đồng hồ thông minh|smartwatch|smart watch|apple watch|vòng đeo`),
		r("phones.headphones", `tai nghe`), r("home.audio", `loa`),
		r("laptop.network", network), r("home.tools", `nguồn meanwell|bộ nguồn tổ ong`),
		r("laptop.parts", `chuột|bàn phím|lót chuột|bàn di|ổ cứng|usb|thẻ nhớ|hub`),
		r("laptop.other", `laptop|macbook|máy tính`),
	}, "phones.accessories"},
	"Trò chơi điện tử & Gaming": {[]rule{r("laptop.parts", `chuột|bàn phím|tai nghe|lót chuột`)}, "hobby.games"},
	"Phần mềm & Hàng hóa số":    {nil, "laptop.other"},

	// Fashion.
	"Thời trang & Quần áo": {[]rule{
		r("baby.clothes", `bé|trẻ em|kids|sơ sinh`), r("fashion.men", `(^|\s)nam(\s|$|,)|(^|\s)men(\s|$)`),
	}, "fashion.women"},
	"Giày dép":        {nil, "fashion.shoes"},
	"Túi xách & Vali": {nil, "fashion.bags"},
	"Đồng hồ":         {[]rule{r("phones.watch", `thông minh|smart|apple watch|garmin|galaxy watch|huawei watch`)}, "fashion.watches"},
	"Trang sức":       {nil, "fashion.jewelry"},
	"Phụ kiện thời trang": {[]rule{
		r("fashion.watches", `dây đồng hồ|đồng hồ`), r("fashion.jewelry", `nhẫn|dây chuyền|bông tai|khuyên tai|lắc|vòng tay|kẹp tóc`),
		r("fashion.bags", `túi|(^|\s)ví(\s|$)|balo`),
		r("fashion.accessories", `kính|(^|\s)mũ|nón|thắt lưng|dây nịt|cà vạt|cavat|khăn|găng tay|tất|vớ|ghim cài|băng đô|bờm|nơ|cài áo`),
	}, "fashion.other"},

	// Home and furniture.
	"Nội thất gia đình": {[]rule{
		r("home.bedding", bedding), r("furniture.storage", `két sắt`),
		r("furniture.sofa", `sofa|ghế băng`), r("furniture.chair", `^(bộ )?ghế|(^|\s)ghế (công thái học|văn phòng|xoay|ăn|xếp|gỗ|nhựa|đôn|bệt|thư giãn|gaming)`),
		r("furniture.bed", `giường|nệm|đệm`), r("furniture.table", `bàn`),
		r("furniture.storage", `tủ|kệ|giá|rương`), r("home.tools", `đèn pin`), r("home.other", `muỗi|côn trùng`),
		r("furniture.lighting", `đèn`),
	}, "furniture.other"},
	"Chăn ga gối đệm & Phòng tắm": {[]rule{
		r("furniture.bed", `giường`), r("home.bedding", bedding+`|nệm|đệm|khăn tắm|rèm`),
		r("home.cleaning", `cây lau|chổi|thùng rác|cây lăn bụi`),
	}, homeOther},
	"Nhà bếp & Ăn uống": {nil, "home.kitchen"},
	"Thiết bị gia dụng lớn": {[]rule{
		r("home.washer", `máy giặt|máy sấy`), r("home.aircon", `điều hòa|máy lạnh`), r("home.fan", `quạt`),
		r("home.kitchen", `tủ lạnh|tủ đông|tủ mát|lò|bếp|máy rửa bát|máy rửa chén|nồi`), r("home.audio", `loa|tivi|smart tv|android tv`),
		r("home.cleaning", `máy hút|robot|máy lọc không khí|máy lọc khí|máy lau|máy hút ẩm|máy tạo ẩm`),
	}, homeOther},
	"Thiết bị gia dụng nhỏ": {[]rule{
		r("home.fan", `quạt`), r("home.kitchen", `nồi|ấm|bếp|lò|máy xay|máy ép|bình đun|máy pha|chảo`),
		r("beauty.devices", `máy sấy tóc|máy cạo|máy massage|bàn chải điện|tông đơ`),
		r("home.cleaning", `máy hút|robot|máy lọc|cây lau|chổi|thùng rác|máy lau|cây lăn bụi`),
		r("home.tools", `ổ cắm|công tắc|cầu dao|ổ khóa|khóa cửa|khóa vân tay|khóa thông minh|bóng đèn|dây điện`),
	}, homeOther},
	"Trang trí nhà cửa & Đèn chiếu sáng": {[]rule{
		r("home.tools", `ổ cắm|công tắc|cầu dao|bóng đèn|đèn pin`), r("home.other", `muỗi|côn trùng`),
		r("home.garden", `năng lượng mặt trời|đèn (đường|pha|sân|trụ cổng)`), r("furniture.lighting", `đèn`),
	}, "home.decor"},
	"Sân vườn & Ngoại thất": {[]rule{r("sports.camping", `lều|cắm trại|camping`)}, "home.garden"},
	"Công cụ & Phần cứng":   {[]rule{r("vehicles.parts", `xe máy|ô tô|oto`)}, "home.tools"},

	// Family and hobbies.
	"Mẹ & Bé": {[]rule{
		r("baby.stroller", `xe đẩy|nôi|địu`), r("baby.highchair", `ghế ăn|ghế ngồi`), r("baby.toys", `đồ chơi|xếp hình|lego`),
		r("baby.clothes", `áo|quần|váy|đầm|body|yếm|mũ|nón|giày|dép`),
	}, "baby.other"},
	"Đồ chơi & Trò chơi": {[]rule{
		r("hobby.collectibles", `mô hình|figure|one piece|gundam|lego|bộ sưu tập`),
		r("hobby.other", `bài oracle|tarot|board game|boardgame|(^|\s)cờ(\s|$)|rubik`),
	}, "baby.toys"},
	"Nhạc cụ": {nil, "hobby.music"},
	"Nghệ thuật & Thủ công": {[]rule{
		r("fashion.accessories", `móc (khóa|khoá|chìa)`), r("books.stationery", `túi giấy|túi kraft|thiệp|sticker|bookmark|hộp quà`),
		r("home.kitchen", `bình nước|cốc|ly|bình giữ nhiệt`),
		r("hobby.collectibles", `mô hình|tượng|figure|sưu tầm`), r("home.decor", `cây thông|treo|trang trí|hộp nhạc`),
	}, "hobby.crafts"},
	"Đồ dùng cho thú cưng": {[]rule{r("pets.homes", `chuồng|lồng|nệm|(^|\s)ổ(\s|$)|nhà cho|balo|túi vận chuyển`)}, "pets.supplies"},
	"Thể thao & Dã ngoại": {[]rule{
		r("sports.racket", `vợt|cầu lông|tennis|pickleball|bóng bàn`), r("sports.football", `bóng đá|giày đá|găng thủ môn`),
		r("sports.camping", `lều|cắm trại|camping|túi ngủ|dã ngoại|câu cá`),
		r("sports.gym", `(^|\s)tạ(\s|$)|gym|yoga|máy chạy|xà đơn|thảm tập|dây kháng lực`),
		r("sports.gym", `xe đạp tập|máy tập|bó gối|băng bảo vệ|đai bảo vệ`), r("vehicles.bicycle", `xe đạp`),
		r("hobby.collectibles", `bật lửa|zippo`),
		r("sports.clothing", `^(bộ )?(áo|quần|giày|đồ|tất|vớ|váy)|giày (chạy|thể thao|tập)`),
	}, "sports.other"},

	// Beauty and health.
	"Sắc đẹp & Chăm sóc cá nhân": {[]rule{
		r("beauty.perfume", `nước hoa|perfume|body mist`), r("beauty.devices", `máy|bàn chải điện`),
		r("beauty.hair", `tóc`), r("beauty.makeup", `son|phấn|mascara|kẻ mắt|kẻ chân mày|cushion|trang điểm|má hồng|cọ`),
		r("beauty.skincare", `kem|serum|toner|sữa rửa mặt|mặt nạ|dưỡng|chống nắng|tẩy`),
	}, "beauty.other"},
	"Sức khỏe & Thể chất": {[]rule{
		r("beauty.devices", `máy|massage|cân sức khỏe|cân điện tử|nhiệt kế|huyết áp`), r("sports.gym", `(^|\s)tạ(\s|$)|dụng cụ tập|yoga|đai`),
	}, ""},

	// Books.
	"Sách & Ấn phẩm": {[]rule{
		r("books.comics", `truyện tranh|manga|comic|tập \d|doraemon|conan`),
		r("books.study", `sách giáo khoa|luyện thi|ielts|toeic|toán|tiếng anh|đề thi|ngữ pháp|từ vựng|lớp \d`),
	}, "books.books"},
	"Văn phòng phẩm & Dụng cụ học tập": {[]rule{
		r("laptop.printers", `máy in|máy chiếu|máy hủy`), r("fashion.bags", `ba lô|balo|cặp`),
	}, "books.stationery"},

	// Vehicles.
	"Ô tô & Xe máy": {[]rule{
		r("vehicles.bicycle", `xe đạp`), r("vehicles.motorbike", `^xe (moto|máy)|honda (cbr|sh|vision|wave|air blade)|yamaha (exciter|r15)|vespa|super soco|xe điện`),
		r("vehicles.car", `^xe ô tô|^ô tô (toyota|honda|kia|mazda|ford|vinfast|hyundai)`),
	}, "vehicles.parts"},

	// Not second-hand goods.
	"Tạp hóa & Thực phẩm":      {nil, ""},
	"Công nghiệp & Thương mại": {nil, ""},
}

// bedding is what Chăn ga, gối nệm holds wherever a store filed it.
const bedding = `chăn|(^|\s)ga (chống|giường|trải|bọc)|bộ ga|gối|drap|chiếu|màn chụp|mùng|mền|ruột gối|vỏ gối|áo gối`

// network is the gear that belongs to Thiết bị mạng wherever a store filed it.
const network = `router|wifi|wi-fi|bộ phát|cáp mạng|(^|\s)lan(\s|$)|switch|modem|bộ chia mạng|tp-link|tp link|mesh|access point|đầu bấm mạng|cat ?[5-7]`

// roots catch listings filed under an old root instead of a leaf.
var roots = map[string]string{
	"Laptop & Máy tính": "Máy tính & Thiết bị mạng", "Nội thất & Trang trí": "Nội thất gia đình",
	"Sách & Văn phòng phẩm": "Sách & Ấn phẩm", "Thời trang & Phụ kiện": "Thời trang & Quần áo",
	"Xe cộ & Phụ tùng": "Ô tô & Xe máy", "Đồ gia dụng": "Thiết bị gia dụng nhỏ",
}

// Category returns the leaf a listing of the old category goes to, or false
// when the listing is dropped.
func Category(oldCategory, title string) (string, bool) {
	if consumable.MatchString(title) {
		return "", false
	}
	if alias, ok := roots[oldCategory]; ok {
		oldCategory = alias
	}
	rt, ok := routes[oldCategory]
	if !ok {
		return "", false
	}
	for _, x := range rt.rules {
		if x.re.MatchString(title) {
			return x.leaf, true
		}
	}
	return rt.fallback, rt.fallback != ""
}

// junk is what store titles carry and a person's would not: "- Hàng",
// "(Hàng )", "Hàng nhập khẩu" and the like, left where "chính hãng" was cut.
var junk = regexp.MustCompile(`(?i)\s*[-–(\[{]*\s*hàng\s*(nhập khẩu|nhập|công ty|cao cấp|loại 1)?\s*[)\]}]*\s*$|\{[^}]*\}|\[[^\]]*\]`)

var spaces = regexp.MustCompile(`\s+`)

// CleanTitle strips store junk, softens an all-caps title and cuts it to
// max runes at a word boundary.
func CleanTitle(title string, maxRunes int) string {
	t := title
	for range 3 {
		t = strings.TrimSpace(junk.ReplaceAllString(t, ""))
	}
	t = strings.Trim(spaces.ReplaceAllString(t, " "), " -–,.:;|/")
	if shouting(t) {
		t = sentence(t)
	}
	rs := []rune(t)
	if len(rs) <= maxRunes {
		return t
	}
	cut := string(rs[:maxRunes])
	if i := strings.LastIndexAny(cut, " ,-–(/"); i > maxRunes/2 {
		cut = cut[:i]
	}
	// A bracket the cut left open goes, with what follows it.
	if i := strings.LastIndexAny(cut, "(["); i > len(cut)/3 && !strings.ContainsAny(cut[i:], ")]") {
		cut = cut[:i]
	}
	return strings.Trim(cut, " -–,.:;|/(")
}

// shouting reports a title whose letters are mostly capitals.
func shouting(s string) bool {
	upper, letters := 0, 0
	for _, c := range s {
		if unicode.IsLetter(c) {
			letters++
			if unicode.IsUpper(c) {
				upper++
			}
		}
	}
	return letters >= 8 && upper*10 >= letters*7
}

// sentence lowercases s but its first letter and words with digits, which
// are model codes.
func sentence(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if strings.ContainsAny(w, "0123456789") {
			continue
		}
		w = strings.ToLower(w)
		if i == 0 {
			r, size := utf8.DecodeRuneInString(w)
			w = string(unicode.ToUpper(r)) + w[size:]
		}
		words[i] = w
	}
	return strings.Join(words, " ")
}
