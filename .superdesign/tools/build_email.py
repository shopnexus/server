"""Mockups of the mail the server sends, one HTML file per mail in mockups/.

Tables and inline styles, as a mail client needs them, so the markup ports to
the server's html/template. Run from server/.superdesign: python3 tools/build_email.py
"""
from html import escape
from itertools import count
from pathlib import Path

PROJECT = "46854d87-b0a2-44e8-9015-e73e5f75b048"
SITE = "https://shopnexus.id.vn"
LOGO = ("https://vgbujcuwptvheqijyjbe.supabase.co/storage/v1/object/public/hmac-uploads/projects/"
        "46854d87-b0a2-44e8-9015-e73e5f75b048/brand-assets/brand-web-brand_mark.png/brand_mark.png")
# The mascot drawings, uploaded once to the website's project (website/.superdesign/tools/art_urls.json).
ART_BASE = ("https://vgbujcuwptvheqijyjbe.supabase.co/storage/v1/object/public/hmac-uploads/projects/"
            "af33e77d-fa0e-408b-a30c-0ae8731c173e/content-assets/app-art-{0}.webp/{0}.webp")

FONT = "'Be Vietnam Pro',-apple-system,'Segoe UI',Roboto,Arial,sans-serif"
MONO = "ui-monospace,'SF Mono',Menlo,Consolas,monospace"
INK, BODY, MUTED, LINE = "#121817", "#3E4947", "#6E7977", "#E1E6E4"
PRIMARY, DONE, HERO, TONAL, PAGE = "#005049", "#066A61", "#DDEFEB", "#F1F4F2", "#EDF1EF"

# Mock order, the same as the app's Đơn COD mockup (app/.superdesign/tools/build_cod_order.py).
ITEM = dict(title="iPhone X 64GB, pin 86%", short="iPhone X",
            photo="https://images.unsplash.com/photo-1510557880182-3d4d3cba35a5?w=192&q=80&auto=format&fit=crop",
            price="3.800.000 ₫", fee="22.000 ₫", total="3.822.000 ₫")
SELLER, BUYER = "Hoàng Nam", "Khánh Linh"
ORDER = f"{SITE}/orders/0193f2a4-7c1e-7b2a-9d41-5e8c2f6a1b37"
CODE = "S1.A1.17373471"
PHASES = ["Chốt giá", "Tạo đơn", "Nhận hàng", "Đánh giá"]

_ids = count(1)


def link(href, inner, style, name):
    return f'<a id="{name}-{next(_ids)}" href="{href}" style="{style}">{inner}</a>'


def art(name):
    return ART_BASE.format(name)


def item_row(side, who):
    """The listing: photo, title, price, and the other side of the deal."""
    return f"""
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:separate;border:1px solid {LINE};border-radius:16px">
  <tr>
    <td width="64" style="padding:12px 0 12px 12px;vertical-align:middle">
      <img src="{ITEM['photo']}" width="64" height="64" alt="{escape(ITEM['title'])}" style="display:block;width:64px;height:64px;border-radius:12px;object-fit:cover">
    </td>
    <td style="padding:12px 14px;vertical-align:middle">
      <div style="font:600 15px/22px {FONT};color:{INK}">{escape(ITEM['title'])}</div>
      <div style="font:600 15px/22px {FONT};color:{INK};font-variant-numeric:tabular-nums">{ITEM['price']}</div>
      <div style="font:400 13px/18px {FONT};color:{MUTED}">{side} · {who}</div>
    </td>
  </tr>
</table>"""


def track(done, current=None):
    """The four steps of a COD order: `done` steps carry a check, `current` a ring.

    Each step is a column with the line's two halves either side of its dot,
    so labels sit under their dots without overlapping cells."""
    reached = current if current is not None else done - 1
    cells = []
    for i, name in enumerate(PHASES):
        if i < done:
            dot = (f'<div style="width:24px;height:24px;border-radius:12px;background:{DONE};color:#FFFFFF;'
                   f'font:700 13px/24px {FONT};text-align:center">✓</div>')
        elif i == current:
            dot = (f'<div style="width:20px;height:20px;border-radius:12px;border:2px solid {DONE};background:#FFFFFF">'
                   f'<div style="width:8px;height:8px;border-radius:4px;background:{DONE};margin:6px auto 0"></div></div>')
        else:
            dot = f'<div style="width:20px;height:20px;border-radius:12px;border:2px solid {LINE};background:#FFFFFF"></div>'
        left = "transparent" if i == 0 else (DONE if i <= reached else LINE)
        right = "transparent" if i == len(PHASES) - 1 else (DONE if i + 1 <= reached else LINE)
        color = INK if i <= reached else MUTED
        weight = 600 if i == current else 500
        cells.append(f"""<td width="25%" style="vertical-align:top;text-align:center;padding:0">
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
    <td style="padding:0"><div style="height:2px;background:{left}"></div></td>
    <td width="24" style="padding:0">{dot}</td>
    <td style="padding:0"><div style="height:2px;background:{right}"></div></td>
  </tr></table>
  <div style="font:{weight} 12px/16px {FONT};color:{color};margin-top:8px">{name}</div></td>""")
    return f'<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>{"".join(cells)}</tr></table>'


def rows(pairs):
    """Detail rows: label at left, value at right, a line between."""
    out = []
    for i, (label, value) in enumerate(pairs):
        border = f"border-top:1px solid {LINE};" if i else ""
        out.append(f'<tr><td style="{border}padding:12px 0;font:400 14px/20px {FONT};color:{MUTED}">{label}</td>'
                   f'<td align="right" style="{border}padding:12px 0;font:500 14px/20px {FONT};color:{INK};font-variant-numeric:tabular-nums">{value}</td></tr>')
    return f'<table role="presentation" width="100%" cellpadding="0" cellspacing="0">{"".join(out)}</table>'


def note(text):
    return (f'<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>'
            f'<td style="background:{TONAL};border-radius:16px;padding:14px 16px;font:400 14px/20px {FONT};color:{BODY}">{text}</td></tr></table>')


def code_panel(code):
    spaced = code[:3] + "&nbsp;&nbsp;" + code[3:]
    return (f'<table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>'
            f'<td align="center" style="background:{HERO};border-radius:20px;padding:22px 16px">'
            f'<div style="font:600 40px/48px {FONT};letter-spacing:6px;color:{PRIMARY};font-variant-numeric:tabular-nums">{spaced}</div>'
            f'<div style="font:400 13px/18px {FONT};color:{BODY};margin-top:6px">Hết hạn sau 5 phút</div></td></tr></table>')


def button(label, href):
    return (f'<table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 auto"><tr>'
            f'<td style="border-radius:9999px;background:{PRIMARY}">'
            + link(href, label, f"display:inline-block;padding:14px 32px;font:600 15px/20px {FONT};color:#FFFFFF;text-decoration:none;border-radius:9999px", "cta")
            + '</td></tr></table>')


def stars(href):
    """Five stars, each a link that opens the review with that rating."""
    cells = "".join(
        f'<td style="padding:0 4px">' + link(f"{href}?stars={n}", "★",
                                              f"display:block;width:44px;height:44px;border-radius:22px;background:{HERO};color:{DONE};font:400 24px/44px {FONT};text-align:center;text-decoration:none",
                                              f"star{n}") + '</td>'
        for n in range(1, 6))
    return f'<table role="presentation" cellpadding="0" cellspacing="0" style="margin:0 auto"><tr>{cells}</tr></table>'


def gap(px):
    return f'<div style="height:{px}px;line-height:{px}px;font-size:0">&nbsp;</div>'


def page(*, group, preheader, art_name, title, lead, blocks, cta=None, order_mail=False, why):
    body = "".join(gap(20) + b for b in blocks)
    if cta:
        body += gap(28) + button(*cta)
    footer_links = [("Trợ giúp", f"{SITE}/help")]
    if order_mail:
        footer_links.insert(0, ("Tắt email đơn hàng", f"{SITE}/notifications#settings"))
    links = " &nbsp;·&nbsp; ".join(link(h, t, f"color:{BODY};text-decoration:underline", "foot") for t, h in footer_links)
    return f"""<!DOCTYPE html>
<html lang="vi">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light">
<title>{escape(title)}</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Be+Vietnam+Pro:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
  @media (max-width: 480px) {{
    .wrap {{ padding: 16px 8px 32px !important; }}
    .inner {{ padding: 0 20px 28px !important; }}
  }}
</style>
</head>
<body style="margin:0;padding:0;background:{PAGE}">
<div style="background:{PAGE};min-height:100vh;width:100%">
  <div style="display:none;max-height:0;overflow:hidden;opacity:0">{escape(preheader)}</div>
  <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:{PAGE}">
    <tr><td align="center" class="wrap" style="padding:32px 16px 40px">
      <table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px">
        <tr><td style="padding:0 8px 16px">
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0"><tr>
            <td style="vertical-align:middle"><img src="{LOGO}" width="28" height="28" alt="ShopNexus" style="display:inline-block;vertical-align:middle;border-radius:8px">
              <span style="font:700 17px/28px {FONT};color:{INK};vertical-align:middle;margin-left:8px;letter-spacing:-0.2px">ShopNexus</span></td>
            <td align="right" style="vertical-align:middle;font:500 13px/18px {FONT};color:{MUTED}">{group}</td>
          </tr></table>
        </td></tr>
        <tr><td style="background:#FFFFFF;border:1px solid {LINE};border-radius:24px;overflow:hidden">
          <table role="presentation" width="100%" cellpadding="0" cellspacing="0">
            <tr><td align="center" style="background:{HERO};padding:24px 24px 0;border-radius:24px 24px 0 0">
              <img src="{art(art_name)}" width="168" height="168" alt="" style="display:block;width:168px;height:168px">
            </td></tr>
            <tr><td class="inner" style="padding:0 32px 32px">
              {gap(28)}
              <div style="font:600 24px/32px {FONT};color:{INK};letter-spacing:-0.2px">{title}</div>
              <div style="font:400 16px/24px {FONT};color:{BODY};margin-top:8px">{lead}</div>
              {body}
            </td></tr>
          </table>
        </td></tr>
        <tr><td align="center" style="padding:24px 16px 0;font:400 13px/20px {FONT};color:{MUTED}">
          {why}<br>{links}
          <div style="margin-top:16px;font:500 12px/16px {FONT};color:{MUTED}">ShopNexus · Mua bán dễ dàng, xem hàng rồi mới trả</div>
        </td></tr>
      </table>
    </td></tr>
  </table>
</div>
</body>
</html>
"""


ORDER_WHY = "Bạn nhận email này vì có đơn hàng trên ShopNexus."

MAILS = {
    "code-sign-in": dict(
        title="Mã đăng nhập", width=640, height=900,
        html=lambda: page(
            group="Bảo mật", preheader="Mã 583 920, hết hạn sau 5 phút. Đừng đưa mã cho ai.",
            art_name="art_verified", title="Mã đăng nhập ShopNexus",
            lead="Nhập mã này trên ứng dụng hoặc trang web để đăng nhập.",
            blocks=[code_panel("583920"),
                    note("Đừng đưa mã cho ai, kể cả người nói là nhân viên ShopNexus. Nếu bạn không yêu cầu mã, cứ bỏ qua email này.")],
            why="Ai đó vừa yêu cầu mã đăng nhập bằng địa chỉ này.")),
    "code-add-email": dict(
        title="Mã thêm email", width=640, height=940,
        html=lambda: page(
            group="Bảo mật", preheader="Mã 271 604 để thêm email này vào một tài khoản ShopNexus.",
            art_name="art_verified", title="Thêm email vào tài khoản",
            lead="Mã này thêm <b style=\"font-weight:600;color:#121817\">khanhlinh@gmail.com</b> vào một tài khoản ShopNexus đang đăng nhập.",
            blocks=[code_panel("271604"),
                    note("Người có mã sẽ đăng nhập được bằng email của bạn. Nếu bạn không yêu cầu, đừng đưa mã cho ai.")],
            why="Ai đó vừa yêu cầu thêm địa chỉ này vào tài khoản ShopNexus.")),
    "order-cod-requested": dict(
        title="Đơn COD mới (người bán)", width=640, height=1180,
        html=lambda: page(
            group="Đơn hàng", preheader=f"{BUYER} chọn nhận COD {ITEM['short']} giá {ITEM['price']}. Tạo đơn để gửi hàng.",
            art_name="art_orders", title=f"{BUYER} chọn nhận COD",
            lead="Tạo đơn để gửi hàng. Người mua xem hàng rồi mới trả shipper.",
            blocks=[item_row("Người mua", BUYER), track(1, 1),
                    rows([("Giao tới", "Phường Bình Thạnh, TP.HCM")]),
                    note("Bên vận chuyển chuyển tiền hàng cho bạn 1–2 ngày sau khi người mua nhận.")],
            cta=("Tạo đơn", ORDER), order_mail=True, why=ORDER_WHY)),
    "order-shipped": dict(
        title="Đã gửi hàng (người mua)", width=640, height=1140,
        html=lambda: page(
            group="Đơn hàng", preheader=f"{SELLER} đã gửi {ITEM['short']} qua GHTK, mã {CODE}.",
            art_name="art_cod_placed", title=f"{SELLER} đã gửi {ITEM['short']}",
            lead="Xem hàng khi shipper tới, đúng thì mới trả tiền.",
            blocks=[item_row("Người bán", SELLER), track(2, 2),
                    rows([("Hãng vận chuyển", "GHTK"),
                          ("Mã vận đơn", f'<span style="font-family:{MONO};letter-spacing:0.3px">{CODE}</span>'),
                          ("Trả khi nhận", f'<span style="font-weight:600">{ITEM["total"]}</span>')])],
            cta=("Xem đơn hàng", ORDER), order_mail=True, why=ORDER_WHY)),
    "order-delivered-buyer": dict(
        title="Đã giao (người mua)", width=640, height=1040,
        html=lambda: page(
            group="Đơn hàng", preheader=f"{ITEM['short']} đã giao. Chấm {SELLER} mấy sao?",
            art_name="art_received", title=f"{ITEM['short']} đã giao tới bạn",
            lead=f"{SELLER} bán thế nào? Chấm sao để người mua sau biết.",
            blocks=[stars(ORDER + "/review"), item_row("Người bán", SELLER), track(3, 3)],
            cta=None, order_mail=True, why=ORDER_WHY)),
    "order-received-seller": dict(
        title="Người mua đã nhận (người bán)", width=640, height=1060,
        html=lambda: page(
            group="Đơn hàng", preheader=f"{BUYER} đã nhận {ITEM['short']}. Tiền hàng về trong 1–2 ngày.",
            art_name="art_deal", title=f"{BUYER} đã nhận {ITEM['short']}",
            lead="Đơn đã xong. Đánh giá người mua khi bạn rảnh.",
            blocks=[item_row("Người mua", BUYER), track(3, 3),
                    note("Bên vận chuyển chuyển tiền hàng cho bạn 1–2 ngày sau khi người mua nhận.")],
            cta=("Đánh giá người mua", ORDER), order_mail=True, why=ORDER_WHY)),
    "order-failed-seller": dict(
        title="Giao không thành (người bán)", width=640, height=1080,
        html=lambda: page(
            group="Đơn hàng", preheader=f"{ITEM['short']} giao không thành, hàng đang hoàn về bạn.",
            art_name="art_not_found", title=f"{ITEM['short']} giao không thành",
            lead="Người mua không nhận hàng. Bưu kiện đang hoàn về bạn.",
            blocks=[item_row("Người mua", BUYER),
                    rows([("Hãng vận chuyển", "GHTK"),
                          ("Mã vận đơn", f'<span style="font-family:{MONO};letter-spacing:0.3px">{CODE}</span>')]),
                    note("Phí giao cả hai chiều do GHTK tính vào tài khoản của bạn. Tin đăng đã mở bán lại.")],
            cta=("Xem đơn hàng", ORDER), order_mail=True, why=ORDER_WHY)),
    "order-cancelled-buyer": dict(
        title="Đơn đã huỷ (người mua)", width=640, height=1000,
        html=lambda: page(
            group="Đơn hàng", preheader=f"Người bán chưa gửi {ITEM['short']}, đơn đã huỷ. Bạn chưa trả gì.",
            art_name="art_search", title="Đơn của bạn đã huỷ",
            lead=f"{SELLER} chưa gửi {ITEM['short']} nên đơn tự huỷ. Bạn chưa trả gì nên không có gì phải hoàn.",
            blocks=[item_row("Người bán", SELLER)],
            cta=("Tìm món tương tự", f"{SITE}/search?q=iPhone%20X"), order_mail=True, why=ORDER_WHY)),
    "account-suspended": dict(
        title="Tài khoản bị khoá", width=640, height=980,
        html=lambda: page(
            group="Tài khoản", preheader="Tài khoản của bạn bị khoá đến 30/11. Bạn vẫn xem tin và đơn được.",
            art_name="art_offline", title="Tài khoản của bạn bị khoá đến 30/11",
            lead="Trong thời gian này bạn vẫn xem tin và đơn hàng, nhưng chưa đăng tin, nhắn tin hay trả giá được.",
            blocks=[note("Nếu bạn nghĩ đây là nhầm lẫn, gửi yêu cầu hỗ trợ kèm lời giải thích của bạn.")],
            cta=("Gửi yêu cầu hỗ trợ", f"{SITE}/support"), why="Email về tài khoản luôn được gửi, kể cả khi bạn tắt email đơn hàng.")),
    "account-unsuspended": dict(
        title="Tài khoản đã mở khoá", width=640, height=860,
        html=lambda: page(
            group="Tài khoản", preheader="Tài khoản của bạn đã mở khoá. Bạn đăng tin và nhắn tin lại được.",
            art_name="art_posted", title="Tài khoản của bạn đã mở khoá",
            lead="Bạn lại đăng tin, nhắn tin và trả giá được như trước.",
            blocks=[], cta=("Mở ShopNexus", SITE), why="Email về tài khoản luôn được gửi, kể cả khi bạn tắt email đơn hàng.")),
}

if __name__ == "__main__":
    out = Path(__file__).resolve().parent.parent / "mockups"
    out.mkdir(exist_ok=True)
    for name, m in MAILS.items():
        (out / f"{name}.html").write_text(m["html"](), encoding="utf-8")
        print(name)
