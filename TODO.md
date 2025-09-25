- xoá hết field code, dùng "slug" instead và chỉ những table cần SEO thì mới thêm còn lại vẫn sử dụng int64 autoincrease để identify record
- dùng hashid để encode/decode id khi expose ra ngoài
- thêm field is primary vô trong sku => để tính toán flagship nhanh hơn thay vì tìm minimum
- fix lỗi ko có inventory thì sẽ ko tìm đc flagship => panic

## Recommendation system

1. Chuẩn bị dữ liệu
   Bảng Product:

Khi vendor thêm sản phẩm mới, hệ thống sẽ tính và lưu TF-IDF vector mô tả sản phẩm (dựa trên tên, mô tả, tags...).

Bảng Cosine Similarity:

Hằng ngày (hoặc khi có sản phẩm mới), hệ thống tính cosine similarity giữa sản phẩm mới và các sản phẩm hiện có, lưu kết quả vào bảng để tra cứu nhanh.

2. Tạo danh sách gợi ý

Chọn sản phẩm gốc:

Dựa trên các sản phẩm mà user đã tương tác nhiều nhất (view, add-to-cart, purchase...).

Tìm sản phẩm tương tự:

Từ các sản phẩm gốc, lấy ra danh sách các sản phẩm có độ tương đồng cao (cosine similarity).