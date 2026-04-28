# note

- should write a blog about should not use default uuid in database. pass id from application layer instead because of
  better control. E.g we can track the just created id before inserting to db without querying back to db or telling the
  db returning back the entity
  (Except for serial handle because we dont use the entity after generated, so just copy from or batch insert it)

- dùng product code thay vì id trong url để lấy product (tăng SEO)
- support no auth list product-card & checkout
- should write a blog about transaction in service layer instead of repository layer (combine with sqlc)

- toàn bộ lib trong infras phải có WithTimeout() trả về chính nó để timeout call
- luôn tạo transaction để db command trong restate run
- wire filter by category (/category/id)
- allow add background image
- allow reference item in chat

- nên mã hoá các data nhạy cảm như credit card, service option config trong db, tránh lưu plain object
- những hàm nào ko có side effect external module thì nên bọc trong transaction, mặc định là phải bọc các side effect database ở trong restate.Run để journal nhưng nếu ko gọi external service/module thì ko cần bọc, chỉ cần atomic operation là đc

- trong refund session wallet dùng currency A, sau đó user đổi sang currency B trước thời điểm refund -> phải convert -> ko nên cho đổi currency khi đang có refund hoặc đang hold escrow

- chỉ dùng workflow nếu cần saga pattern trong xuyên suốt quá trình tạo entity đó, ví dụ tạo payment session (checkout), tạo order (order confirm), 1 step fail cần đc rollback toàn tuần tự
