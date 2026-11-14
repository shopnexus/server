"""Builds data/seed.json and data/photos from the app's fixtures; run from server/ as
python3 -I cmd/devseed/data/build.py. The output is committed, so the seed does not need the app."""
import hashlib, json, os, shutil, sys

APP = os.path.expanduser("~/shopnexus/app")
FIX = f"{APP}/core/data/src/main/resources/fixtures"
PHOTOS = f"{APP}/core/testing/src/main/resources/photos"
OUT = "cmd/devseed/data"

COMMUNE = {
    "Bình Thạnh": "26929", "Quận 1": "26740", "Phú Nhuận": "27073", "Tân Bình": "27004", "Thủ Đức": "26824",
    "Gò Vấp": "26884", "Quận 3": "27154", "Quận 7": "27487", "Quận 10": "27190", "Quận 5": "27343",
    "Hà Nội": "00070", "Đà Nẵng": "20242", "Cần Thơ": "31135", "Củ Chi": "27553", "Huế": "19753",
}
STREET = {
    "Bình Thạnh": "120 Xô Viết Nghệ Tĩnh", "Gò Vấp": "45 Quang Trung", "Phú Nhuận": "88 Phan Xích Long",
    "Tân Bình": "210 Cộng Hòa", "Quận 3": "15 Kỳ Đồng", "Quận 7": "33 Nguyễn Thị Thập", "Quận 10": "70 Ba Tháng Hai",
    "Quận 5": "12 Trần Hưng Đạo", "Quận 1": "9 Lê Lợi",
}
CATEGORY = {
    "p-iphone13": "phones.iphone", "p-iphone14": "phones.iphone", "p-iphone15": "phones.iphone", "p-iphone12": "phones.iphone",
    "p-mini": "phones.iphone", "p-iphone11": "phones.iphone", "p-iphonex": "phones.iphone", "p-s23": "phones.android",
    "p-s21": "phones.android", "p-redmi": "phones.android", "p-polaroid": "other.other", "p-a7": "other.other",
    "p-xm4": "phones.headphones", "p-airpodspro": "phones.headphones", "p-airpods": "phones.headphones",
    "p-macbook": "laptop.macbook", "p-ipadair": "phones.tablet", "p-ipadpro": "phones.tablet", "p-laptop": "laptop.office",
    "p-watch": "phones.watch", "p-sofa": "furniture.sofa", "p-armchair": "furniture.other", "p-chair": "furniture.other",
    "p-bomber": "fashion.men", "p-nike": "fashion.shoes", "p-r1": "vehicles.motorbike", "p-blender": "home.kitchen",
    "p-toytrain": "baby.toys", "p-babybody": "baby.clothes", "p-dumbbell": "sports.gym", "p-racket": "sports.racket",
    "p-far-guitar": "other.other", "p-far-keyboard": "laptop.other", "p-far-books": "other.other",
    "p-far-stool": "furniture.other", "p-far-shelf": "furniture.storage",
}
CONDITION = {"NEW": "new", "LIKE_NEW": "like_new", "USED": "used"}
WORDS = {
    "new": "Hàng mới nguyên hộp, mua về chưa dùng tới.",
    "like_new": "Đồ còn rất mới, dùng ít và giữ kỹ, không trầy xước đáng kể.",
    "used": "Đồ đã qua sử dụng, hoạt động bình thường, có vài vết dùng nhẹ.",
}
BIOS = [
    "Bán đồ cá nhân dùng kỹ, trả lời tin nhắn buổi tối.",
    "Thanh lý đồ không dùng nữa, giao COD đồng kiểm.",
    "Mê xe đạp, hay lên đời nên bán lại xe cũ.",
    "Đồ công nghệ chính chủ, có hộp và hoá đơn nếu còn.",
]


def photo(url):
    name = hashlib.sha1(url.encode()).hexdigest()[:12] + ".jpg"
    if not os.path.exists(f"{PHOTOS}/{name}"):
        sys.exit(f"no photo for {url}")
    shutil.copy(f"{PHOTOS}/{name}", f"{OUT}/photos/{name}")
    return name


pool = json.load(open(f"{FIX}/pool.json"))
sellers = json.load(open(f"{FIX}/sellers.json"))
people = json.load(open(f"{FIX}/people.json"))
detail = json.load(open(f"{FIX}/listing-p-iphone13.json"))["listing"]
owner = {l: s["seller"]["id"] for s in sellers for l in s["listings"]}
home = {s["seller"]["id"]: s["district"] for s in sellers}

shutil.rmtree(f"{OUT}/photos", ignore_errors=True)
os.makedirs(f"{OUT}/photos")

accounts, phone = [], 900000001
for i, s in enumerate(sellers):
    p = s["seller"]
    if p["id"] == "u-linh":
        continue
    accounts.append({
        "key": p["id"], "name": p["name"], "phone": f"+84{phone}", "bio": BIOS[i % len(BIOS)],
        "avatar": photo(p["avatarUrl"]) if p.get("avatarUrl") else None,
        "street": STREET.get(s["district"], "1 Nguyễn Huệ"), "commune": COMMUNE.get(s["district"], "26740"),
        "carrier": True,
    })
    phone += 1
linh = next(s for s in sellers if s["seller"]["id"] == "u-linh")["seller"]
buyers = [{"id": "u-linh", "name": linh["name"], "avatarUrl": linh.get("avatarUrl")}] + people[:2]
for b in buyers:
    accounts.append({
        "key": b["id"], "name": b["name"], "phone": f"+84{phone}", "bio": "",
        "avatar": photo(b["avatarUrl"]) if b.get("avatarUrl") else None,
        "street": "120 Xô Viết Nghệ Tĩnh", "commune": "26929", "carrier": False,
    })
    phone += 1

listings = []
for item in pool:
    s, f = item["summary"], item["facts"]
    seller = owner.get(s["id"]) or f["sellerId"]
    district = f.get("district") or home[seller]
    condition = CONDITION[s["condition"]]
    entry = {
        "key": s["id"], "seller": seller, "title": s["title"], "price": s["price"],
        "category": CATEGORY.get(s["id"]) or {"BIKE": "vehicles.bicycle"}[f["kind"]], "condition": condition, "allow_offers": s["id"] not in ("p-r1", "p-nike"),
        "commune": COMMUNE[district], "photos": [photo(s["photoUrl"])], "specs": [],
        "description": f"{WORDS[condition]} Xem hàng quanh {district}, hoặc gửi COD đồng kiểm toàn quốc.",
    }
    if s["id"] == detail["id"]:
        extra = [p["url"] for p in detail["photos"] if os.path.exists(f"{PHOTOS}/{hashlib.sha1(p['url'].encode()).hexdigest()[:12]}.jpg")]
        entry["photos"] += [photo(u) for u in extra]
        entry["specs"], entry["description"] = detail["specs"], detail["description"]
    listings.append(entry)

json.dump({"accounts": accounts, "listings": listings}, open(f"{OUT}/seed.json", "w"), ensure_ascii=False, indent=1)
print(len(accounts), "accounts,", len(listings), "listings,", len(os.listdir(f"{OUT}/photos")), "photos")
