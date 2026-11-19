package legacy

import (
	"strings"
	"testing"
)

func TestCategory(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ old, title, want string }{
		{"Điện thoại & Máy tính bảng", "iPhone 13 Pro 128GB", "phones.iphone"},
		{"Mẹ & Bé", "Combo 3 gói Tã quần Huggies", ""},
		{"Mẹ & Bé", "Xe đẩy em bé Joie", "baby.stroller"},
		{"Thời trang & Quần áo", "Áo Thun Nam Cotton", "fashion.men"},
		{"Thời trang & Quần áo", "Chân Váy Halona", "fashion.women"},
		{"Sức khỏe & Thể chất", "Máy Massage Cổ Vai Gáy Kachi", "beauty.devices"},
		{"Sức khỏe & Thể chất", "Miếng dán hỗ trợ trị liệu", ""},
		{"Tạp hóa & Thực phẩm", "Nồi chiên", ""},
		{"Sách & Văn phòng phẩm", "Truyện tranh Conan tập 12", "books.comics"},
		{"Ô tô & Xe máy", "Bánh xe máy Michelin", "vehicles.parts"},
		{"Nội thất gia đình", "Ghế công thái học Sihoo M57", "furniture.chair"},
		{"Nội thất gia đình", "Ghế sofa văn phòng Tundo", "furniture.sofa"},
		{"Nội thất gia đình", "Bộ chăn ga gối cotton", "home.bedding"},
		{"Nội thất gia đình", "Đèn Pin Led Cob chống nước", "home.tools"},
		{"Máy ảnh & Quay phim", "Camera IP wifi Imou Ranger 2C", "camera.security"},
		{"Máy ảnh & Quay phim", "Micro Shotgun cho máy ảnh", "camera.accessories"},
		{"Máy ảnh & Quay phim", "Camera lùi Maxviss RC-101", "vehicles.parts"},
		{"Máy ảnh & Quay phim", "Máy ảnh Fujifilm X-T30", "camera.body"},
		{"Sắc đẹp & Chăm sóc cá nhân", "Xà phòng thảo dược 100g", ""},
		{"Nghệ thuật & Thủ công", "Móc khóa hình mèo", "fashion.accessories"},
	} {
		got, ok := Category(c.old, c.title)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Category(%q, %q) = %q, %v; want %q", c.old, c.title, got, ok, c.want)
		}
	}
}

func TestCleanTitle(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ in, want string }{
		{"PoE Switch Tenda TEF1118P - Hàng", "PoE Switch Tenda TEF1118P"},
		{"Micro có dây MZ-328 dây dài 5m(Hàng )", "Micro có dây MZ-328 dây dài 5m"},
		{"{Kèm quà tặng} Bài Oracle Magical Message", "Bài Oracle Magical Message"},
		{"CÂY BÚT THỬ ĐO KIỂM TRA ĐIỆN DC XE MÁY", "Cây bút thử đo kiểm tra điện dc xe máy"},
		{"Vòi nước vòi phun nước rửa xe tưới cây tăng áp thông minh đầu đồng 206701-2 (bạc dài)", "Vòi nước vòi phun nước rửa xe tưới cây tăng áp thông minh đầu đồng"},
	} {
		if got := CleanTitle(c.in, 70); got != c.want {
			t.Errorf("CleanTitle(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestNearest(t *testing.T) {
	t.Parallel()
	cs, err := Communes()
	if err != nil {
		t.Fatal(err)
	}
	if c := Nearest(cs, 21.0394, 105.8364); c.Code != "00004" {
		t.Errorf("Ba Đình's point is in %+v", c)
	}
	if c := Nearest(cs, 10.7769, 106.7009); c.Province != "79" {
		t.Errorf("central Saigon is in %+v", c)
	}
}

func TestPicker(t *testing.T) {
	t.Parallel()
	cs, err := Communes()
	if err != nil {
		t.Fatal(err)
	}
	p, n := NewPicker(cs), map[string]int{}
	for i := range 10000 {
		u := float64(i) / 10000
		n[p.Pick(u, float64(i%10)/10, float64(i%97)/97).Province]++
	}
	if n["79"] < 3000 || n["79"] > 3800 || n["01"] < 2000 {
		t.Errorf("by province: %v", n)
	}
}

func TestPickerWards(t *testing.T) {
	t.Parallel()
	cs, err := Communes()
	if err != nil {
		t.Fatal(err)
	}
	p := NewPicker(cs)
	for i := range 50 {
		if c := p.Pick(0.01, 0.1, float64(i)/50); !strings.HasPrefix(c.Name, "Phường ") {
			t.Fatalf("a city pick under 0.8 is %q", c.Name)
		}
	}
}

func TestCleanTitleBracket(t *testing.T) {
	t.Parallel()
	in := "Đồ bộ short nữ mùa hè chất thun coton xịn cao cấp Size M (Từ 40kg đến 55kg mặc vừa)"
	if got := CleanTitle(in, 70); got != "Đồ bộ short nữ mùa hè chất thun coton xịn cao cấp Size M" {
		t.Errorf("CleanTitle = %q", got)
	}
}
