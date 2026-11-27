// Package handler serves module payment's HTTP API: the gateway's IPN and
// return page, and the fake gateway's payment page.
package handler

import (
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"uuid"

	"github.com/shopnexus/server/internal/payment/domain"
	"github.com/shopnexus/server/internal/payment/service"
	"github.com/shopnexus/server/internal/platform/httpx"
	"github.com/shopnexus/server/internal/platform/locale"
)

type Handler struct {
	svc service.Service
	log *slog.Logger
}

func New(svc service.Service, log *slog.Logger) *Handler { return &Handler{svc: svc, log: log} }

// Register adds the gateway's routes, open to it, and with the fake the
// dev payment page, open to anyone.
func (h *Handler) Register(r *httpx.Router, fake bool) {
	r.Handle("GET /v1/payments/vnpay/ipn", http.HandlerFunc(h.Notify))
	r.Handle("GET /v1/payments/vnpay/return", http.HandlerFunc(h.Return))
	if fake {
		r.Handle("GET /v1/dev/payments/{id}", http.HandlerFunc(h.DevPage))
		r.Handle("POST /v1/dev/payments/{id}", http.HandlerFunc(h.DevPay))
	}
}

// Notify serves VNPay's IPN; the answer is always 200 with its RspCode.
func (h *Handler) Notify(w http.ResponseWriter, r *http.Request) {
	httpx.Respond(w, http.StatusOK, h.svc.Notify(r.Context(), r.URL.Query()))
}

// page is what the return and dev pages show.
type page struct {
	Lang, Title, Text, Button string
	// Link is the app's own scheme, which html/template would refuse.
	Link        template.URL
	Amount      string
	Pay, Cancel string
}

// pageText is each language's text of the pages.
var pageText = map[locale.Tag]map[string]string{
	locale.Vietnamese: {
		"paid": "Đã thanh toán", "paid_text": "ShopNexus đã nhận tiền. Quay lại ứng dụng để xem gói của bạn.",
		"failed": "Chưa thanh toán", "failed_text": "Giao dịch chưa thành công nên bạn chưa bị trừ tiền.",
		"pending": "Đang chờ xác nhận", "pending_text": "Cổng thanh toán chưa báo kết quả. Ứng dụng sẽ cập nhật sau ít phút.",
		"unknown": "Không tìm thấy giao dịch", "unknown_text": "Liên kết này không còn đúng. Mở ứng dụng để xem lại.",
		"open": "Mở ShopNexus", "dev": "Cổng thanh toán thử", "pay": "Thanh toán", "cancel": "Hủy",
	},
	locale.English: {
		"paid": "Paid", "paid_text": "ShopNexus received the money. Go back to the app to see your package.",
		"failed": "Not paid", "failed_text": "The payment did not go through, so nothing was charged.",
		"pending": "Waiting for confirmation", "pending_text": "The gateway has not reported the result yet. The app updates in a few minutes.",
		"unknown": "Payment not found", "unknown_text": "This link is no longer valid. Open the app to check.",
		"open": "Open ShopNexus", "dev": "Test payment gateway", "pay": "Pay", "cancel": "Cancel",
	},
}

var tmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>body{font-family:system-ui,sans-serif;max-width:28rem;margin:3rem auto;padding:0 1rem;color:#1b1f1e;background:#fff}
a,button{display:block;width:100%;box-sizing:border-box;margin-top:1rem;padding:.8rem;border-radius:.6rem;border:0;font-size:1rem;text-align:center;text-decoration:none}
a,button.pay{background:#0f766e;color:#fff}button.cancel{background:#e6eeec;color:#1b1f1e}</style></head>
<body><h1>{{.Title}}</h1>{{if .Amount}}<p><strong>{{.Amount}}</strong></p>{{end}}<p>{{.Text}}</p>
{{if .Link}}<a href="{{.Link}}">{{.Button}}</a><script>location.href={{.Link}}</script>{{end}}
{{if .Pay}}<form method="post"><button class="pay" name="result" value="pay">{{.Pay}}</button>
<button class="cancel" name="result" value="cancel">{{.Cancel}}</button></form>{{end}}
</body></html>`))

func (h *Handler) render(w http.ResponseWriter, r *http.Request, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if err := tmpl.Execute(w, p); err != nil {
		h.log.WarnContext(r.Context(), "write payment page", "err", err)
	}
}

// Return serves the page VNPay sends the payer back to. It opens the app
// at the product the payment was for: shopnexus://{product}/{id}.
func (h *Handler) Return(w http.ResponseWriter, r *http.Request) {
	lang := locale.Negotiate(w, r)
	t := pageText[lang]
	ret, err := h.svc.Returned(r.Context(), r.URL.Query())
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	p := page{Lang: string(lang), Title: t["unknown"], Text: t["unknown_text"], Button: t["open"], Link: "shopnexus://"}
	if ret.Found {
		key := "pending"
		switch ret.Payment.Status {
		case domain.StatusPaid, domain.StatusRefunding, domain.StatusRefunded:
			key = "paid"
		case domain.StatusFailed, domain.StatusExpired:
			key = "failed"
		case domain.StatusPending:
		default:
		}
		p.Title, p.Text = t[key], t[key+"_text"]
		p.Link = template.URL("shopnexus://" + string(ret.Payment.Product) + "/" + ret.Payment.ProductID.String()) //nolint:gosec // built from stored IDs
	}
	h.render(w, r, p)
}

type devPath struct {
	ID uuid.UUID `json:"-" path:"id"`
}

// DevPage serves the fake's payment page.
func (h *Handler) DevPage(w http.ResponseWriter, r *http.Request) {
	var in devPath
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	p, err := h.svc.DevPayment(r.Context(), in.ID)
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	lang := locale.Negotiate(w, r)
	t := pageText[lang]
	h.render(w, r, page{Lang: string(lang), Title: t["dev"], Amount: locale.Money(lang, p.Amount), Pay: t["pay"], Cancel: t["cancel"]})
}

// DevPay pays or cancels at the fake, then sends the browser to the return
// page as VNPay does.
func (h *Handler) DevPay(w http.ResponseWriter, r *http.Request) {
	var in devPath
	if err := httpx.Bind(r, &in); err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	q, err := h.svc.DevPay(r.Context(), in.ID, r.PostFormValue("result") == "pay")
	if err != nil {
		httpx.WriteError(w, r, h.log, err)
		return
	}
	w.Header().Set("Location", (&url.URL{Path: "/v1/payments/vnpay/return", RawQuery: q.Encode()}).String())
	w.WriteHeader(http.StatusSeeOther)
}
