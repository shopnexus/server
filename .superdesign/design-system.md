# ShopNexus email design system

The mail the server sends: sign-in and add-email codes, order notices
(COD requested, shipped, delivered, failed, cancelled) and account notices
(suspended, unsuspended). The server writes the text (`internal/notification/domain`,
`internal/account/service/email_code_send_durable.go`); this file fixes how it
looks. Mockups: `tools/build_email.py` writes `mockups/*.html`, one per mail;
Superdesign project "ShopNexus Email" (`46854d87-b0a2-44e8-9015-e73e5f75b048`).

Brand, colour roles, copy rules and the deal model come from the app's
`app/.superdesign/design-system.md` and the website's
`website/.superdesign/design-system.md`. This file records only what mail
changes.

## Product rules that reach mail

- ShopNexus holds no money. Mail never mentions a wallet, deposit, escrow or
  a platform refund. The buyer looks at the item at the door and pays the
  shipper; a refused parcel goes back and the buyer pays nothing.
- One mail = one fact the person is waiting for, then one action. No
  marketing blocks, no "AI đang…" narration, no app-store badges.
- Order mail can be turned off (`order_emails`); its footer says where.
  Account and code mail cannot, and their footer says nothing about it.
- A code mail never carries a link: a link next to a code teaches people to
  click links in mail that look like ours.

## Email constraints

- Width 600 px, fluid below it (the card is `width:100%; max-width:600px`).
  Most people read on a phone in the Gmail app, at ~360–412 px.
- Layout with tables and inline styles, so the mockup HTML ports to a Go
  `html/template` almost as is. A `<style>` block carries only the phone
  padding and the dark-mode overrides; clients that drop it still render
  the light layout.
- Font stack: `'Be Vietnam Pro', -apple-system, 'Segoe UI', Roboto, Arial,
  sans-serif`. Apple Mail loads Be Vietnam Pro; Gmail falls back to Roboto or
  Arial, so no layout depends on its metrics.
- Images: PNG served from our domain in production (Outlook drops WebP and
  every client drops SVG). Every image has `alt` text and fixed width and
  height, and the mail reads correctly with images off.
- No icon fonts. Status marks are text (✓, ★) or drawn with table cells.
- A hidden preheader (≤ 90 characters) gives the inbox preview its line.

## Tokens

| Use | Value |
|---|---|
| Page behind the card | `#EDF1EF` |
| Card | `#FFFFFF`, 1 px `#E1E6E4` border, 24 px corners |
| Hero band | `#DDEFEB` (between secondaryContainer `#C7E6E0` and white) |
| Ink | `#121817` |
| Body text | `#3E4947` (onSurfaceVariant) |
| Muted | `#6E7977` (outline) |
| Line | `#E1E6E4` |
| Primary action | `#005049` fill, white text, pill, 48 px tall |
| Done step | `#066A61` fill, white ✓ |
| Tonal panel | `#F1F4F2` (surfaceContainerLow), 16 px corners |
| Code panel | `#DDEFEB`, 20 px corners, digits `#005049` |

No red, no terracotta: a failed delivery, a cancellation and a suspension
use the same neutral card as good news. The words carry the news.

Type: title 24/32 600; lead 16/24 400; body 15/22 400; label 13/18 500;
caption 12/16 400. Money and codes use tabular figures.

## Anatomy

1. Preheader (hidden).
2. Header row above the card: brand mark 28 px and "ShopNexus" at left, the
   mail's group ("Đơn hàng", "Tài khoản", "Bảo mật") at right in muted.
3. Card:
   - Hero band with the mascot drawing that matches the event (from
     `brand/`: `art_orders` new COD order, `art_cod_placed` shipped,
     `art_received` delivered, `art_deal` buyer received (seller),
     `art_not_found` failed delivery, `art_search` cancelled, `art_offline`
     suspended, `art_posted` unsuspended, `art_verified` codes).
   - Title (the fact), lead (what it means for this person).
   - Body blocks as the mail needs: the listing row (photo 64 px, title,
     price, the other side); the four-step track (Chốt giá, Tạo đơn, Nhận
     hàng, Đánh giá, as in the app's Đơn COD); detail rows (label left, value
     right); the code panel; a note on the tonal panel.
   - One primary button.
4. Footer below the card: why the person got the mail, where to turn order
   mail off (order mail only), Trợ giúp, and the tagline.

## Links

Full `https://shopnexus.id.vn/...` URLs to the website's routes: an order
`/orders/<id>`, settings `/notifications#settings`, help `/help`, support
`/support`. The app opens them through its app links.
