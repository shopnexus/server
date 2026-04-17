# TikZ Diagrams Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace 11 placeholder figures in the ShopNexus LaTeX thesis with production-quality TikZ diagrams.

**Architecture:** Each diagram is self-contained TikZ code replacing an `\fbox{\parbox{...}{TODO:...}}` placeholder. Shared styles defined once in `thesis.cls`. Sequence diagrams use `pgf-umlsd` package; all others use core TikZ with standard libraries.

**Tech Stack:** LaTeX, TikZ, pgf-umlsd, thesis.cls (memoir-based)

---

### Task 1: Add TikZ packages to thesis.cls

**Files:**
- Modify: `latex/thesis.cls`

- [ ] **Step 1: Add required packages and TikZ libraries**

In `latex/thesis.cls`, after the existing `\RequirePackage{tikz}` line, add:

```latex
\usetikzlibrary{shapes.geometric, arrows.meta, positioning, fit, backgrounds, calc, decorations.pathreplacing}
\RequirePackage[underline=false]{pgf-umlsd}
```

Note: `\usetikzlibrary{calc}` is already loaded (for cover page). The new line replaces/extends it. The `pgf-umlsd` package is used for sequence diagrams; `underline=false` prevents actor name underlines.

---

### Task 2: fig:tech-stack — Technology Stack Layer Cake (2-theory.tex)

**Files:**
- Modify: `latex/chapters/main/2-theory.tex` — replace the placeholder at `fig:tech-stack`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:tech-stack` (the one with `TODO: Sơ đồ tổng quan technology stack`) with:

```latex
\begin{figure}[htbp]
\centering
\begin{tikzpicture}[
  layer/.style={minimum width=14cm, minimum height=1.1cm, text=white, font=\small\bfseries, rounded corners=3pt, align=center},
  tech/.style={font=\footnotesize, text=black, fill=white, rounded corners=2pt, inner sep=3pt, minimum height=0.6cm},
]
% Layers (bottom to top)
\node[layer, fill=headerblue!50!black] (infra) at (0,0) {};
\node[layer, fill=headerblue!70!black] (data)  at (0,1.4) {};
\node[layer, fill=headerblue!85!black] (back)  at (0,2.8) {};
\node[layer, fill=headerblue]          (api)   at (0,4.2) {};
\node[layer, fill=headerblue!70]       (front) at (0,5.6) {};

% Layer labels (left)
\node[anchor=east, font=\footnotesize\bfseries, text=white] at ([xshift=-.2cm]infra.west) {};
\node[anchor=west, font=\scriptsize, text=white] at ([xshift=.15cm]infra.west) {Hạ tầng};
\node[anchor=west, font=\scriptsize, text=white] at ([xshift=.15cm]data.west) {Dữ liệu};
\node[anchor=west, font=\scriptsize, text=white] at ([xshift=.15cm]back.west) {Backend};
\node[anchor=west, font=\scriptsize, text=white] at ([xshift=.15cm]api.west) {API};
\node[anchor=west, font=\scriptsize, text=white] at ([xshift=.15cm]front.west) {Frontend};

% Tech badges per layer
\node[tech] at ([xshift=1.5cm]front.center) {Next.js 16};
\node[tech] at ([xshift=4.5cm]front.center) {React 19};
\node[tech] at ([xshift=-1.5cm]front.center) {shadcn/ui};
\node[tech] at ([xshift=-3.5cm]front.center) {TanStack Query};

\node[tech] at ([xshift=-1cm]api.center) {ConnectRPC};
\node[tech] at ([xshift=2cm]api.center) {Echo v4};

\node[tech] at ([xshift=-2cm]back.center) {Go 1.26};
\node[tech] at ([xshift=0.5cm]back.center) {Restate};
\node[tech] at ([xshift=3cm]back.center) {Uber fx};
\node[tech] at ([xshift=5cm]back.center) {SQLC};

\node[tech] at ([xshift=-2.5cm]data.center) {PostgreSQL 18};
\node[tech] at ([xshift=0cm]data.center) {Redis 8};
\node[tech] at ([xshift=2cm]data.center) {Milvus 2.6};
\node[tech] at ([xshift=4.5cm]data.center) {MinIO};

\node[tech] at ([xshift=-1cm]infra.center) {Docker Compose};
\node[tech] at ([xshift=2cm]infra.center) {NATS JetStream};
\node[tech] at ([xshift=5cm]infra.center) {Restate Cluster};
\end{tikzpicture}
\caption{Tổng quan technology stack của ShopNexus}
\label{fig:tech-stack}
\end{figure}
```

---

### Task 3: fig:workflow-truyen-thong — Traditional Workflow Activity Diagram (3-analysis/current-state.tex)

**Files:**
- Modify: `latex/chapters/main/3-analysis/current-state.tex` — replace `fig:workflow-truyen-thong`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:workflow-truyen-thong` with:

```latex
\begin{figure}[htbp]
\centering
\begin{tikzpicture}[
  node distance=0.9cm and 2cm,
  act/.style={rectangle, rounded corners=4pt, draw=headerblue, fill=rowgray, text width=3.2cm, minimum height=0.8cm, align=center, font=\footnotesize},
  start/.style={circle, fill=black, minimum size=6pt, inner sep=0pt},
  finish/.style={circle, fill=black, draw=black, minimum size=6pt, inner sep=0pt, double, double distance=1.5pt},
  arr/.style={-{Stealth[length=5pt]}, thick, headerblue!80!black},
  warn/.style={font=\scriptsize\itshape, text=red!70!black, align=center},
]
% Swimlane divider
\draw[dashed, gray] (3.5,1) -- (3.5,-9.5);
\node[font=\small\bfseries, headerblue] at (1.5,0.6) {Khách hàng};
\node[font=\small\bfseries, headerblue] at (5.5,0.6) {Người bán};

% Flow
\node[start] (s) at (1.5,0) {};
\node[act, below=of s] (order) {Gọi điện / đến cửa hàng đặt mua};
\node[act, below=of order, xshift=4cm] (confirm) {Xác nhận thủ công (giấy/sổ)};
\node[act, below=of confirm] (pack) {Đóng gói hàng};
\node[act, below=of pack, xshift=-4cm] (deliver) {Nhận hàng};
\node[act, below=of deliver] (refund) {Liên hệ hoàn trả (gọi/đến)};
\node[act, below=of refund, xshift=4cm] (process) {Xử lý hoàn trả thủ công};
\node[finish] (e) at (5.5,-9) {};

\draw[arr] (s) -- (order);
\draw[arr] (order) -- (confirm);
\draw[arr] (confirm) -- (pack);
\draw[arr] (pack) -- (deliver);
\draw[arr] (deliver) -- (refund);
\draw[arr] (refund) -- (process);
\draw[arr] (process) -- (e);

% Pain point annotations
\node[warn, right=0.3cm of confirm] {dễ sai sót\\không truy vết};
\node[warn, left=0.3cm of refund] {thủ công\\chậm trễ};
\node[warn, right=0.3cm of pack] {không snapshot\\giá sản phẩm};
\end{tikzpicture}
\caption{Lược đồ hoạt động --- Quy trình xử lý đơn hàng truyền thống}
\label{fig:workflow-truyen-thong}
\end{figure}
```

---

### Task 4: fig:workflow-shopnexus — Collaboration Diagram (3-analysis/current-state.tex)

**Files:**
- Modify: `latex/chapters/main/3-analysis/current-state.tex` — replace `fig:workflow-shopnexus`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:workflow-shopnexus` with:

```latex
\begin{figure}[htbp]
\centering
\begin{tikzpicture}[
  actor/.style={rectangle, rounded corners=6pt, draw=headerblue, fill=rowgray, minimum width=2.8cm, minimum height=1cm, align=center, font=\small\bfseries},
  system/.style={rectangle, rounded corners=6pt, draw=headerblue, fill=headerblue!15, minimum width=3.2cm, minimum height=1.2cm, align=center, font=\small\bfseries, line width=1.5pt},
  msg/.style={-{Stealth[length=5pt]}, thick, headerblue!80!black},
  lbl/.style={font=\scriptsize, fill=white, inner sep=2pt, align=center},
]
\node[system] (shop) at (0,0) {ShopNexus};
\node[actor]  (buyer)    at (-5, 1.5) {Khách hàng};
\node[actor]  (seller)   at ( 5, 1.5) {Người bán};
\node[actor]  (payment)  at (-4,-2.5) {Cổng thanh toán};
\node[actor]  (shipping) at ( 4,-2.5) {Đơn vị vận chuyển};

\draw[msg] (buyer)   -- node[lbl, above, sloped] {1. Đặt hàng} (shop);
\draw[msg] (shop)    -- node[lbl, above, sloped] {2. Xác nhận đơn} (seller);
\draw[msg] (buyer)   -- node[lbl, below, sloped] {3. Thanh toán} (payment);
\draw[msg] (payment) -- node[lbl, below] {4. Callback xác nhận} (shop);
\draw[msg] (shop)    -- node[lbl, below, sloped] {5. Tạo vận đơn} (shipping);
\draw[msg] (shipping) -- node[lbl, above, sloped, near end] {6. Giao hàng} (buyer);
\draw[msg, dashed] (buyer) to[bend right=20] node[lbl, below, sloped] {7. Hoàn trả} (shop);
\end{tikzpicture}
\caption{Lược đồ cộng tác --- Mô hình vận hành mới với ShopNexus}
\label{fig:workflow-shopnexus}
\end{figure}
```

---

### Task 5: fig:order-flow — Order + Refund Activity Diagram (2-theory.tex)

**Files:**
- Modify: `latex/chapters/main/2-theory.tex` — replace `fig:order-flow`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:order-flow` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{tikzpicture}[
  node distance=0.7cm,
  act/.style={rectangle, rounded corners=4pt, draw=headerblue, fill=rowgray, text width=2.6cm, minimum height=0.7cm, align=center, font=\scriptsize},
  dec/.style={diamond, draw=headerblue, fill=headerblue!10, aspect=2.2, inner sep=1pt, font=\scriptsize, align=center},
  start/.style={circle, fill=black, minimum size=5pt, inner sep=0pt},
  finish/.style={circle, fill=black, draw=black, minimum size=5pt, inner sep=0pt, double, double distance=1.5pt},
  arr/.style={-{Stealth[length=4pt]}, thick, headerblue!80!black},
  lane/.style={font=\small\bfseries, headerblue},
]
% Swimlanes
\draw[dashed, gray!50] (3.8,0.5) -- (3.8,-14);
\draw[dashed, gray!50] (8,0.5) -- (8,-14);
\node[lane] at (1.8,0.2) {Buyer};
\node[lane] at (5.8,0.2) {ShopNexus};
\node[lane] at (10,0.2) {Seller};

% Main flow
\node[start] (s) at (1.8,-0.3) {};
\node[act, below=of s] (cart) {Thêm vào giỏ hàng};
\node[act, below=of cart] (checkout) {Checkout};
\node[act, below=of checkout, xshift=4cm] (reserve) {Reserve Inventory\\+ Snapshot giá};
\node[act, below=of reserve, xshift=4.2cm] (inbox) {Xem inbox\\pending items};
\node[dec, below=0.8cm of inbox] (d1) {Xác nhận?};
\node[act, below left=0.8cm and 0.5cm of d1] (reject) {Từ chối\\(giải phóng kho)};
\node[act, below right=0.8cm and -0.5cm of d1, xshift=-4.2cm] (create) {Tạo Order +\\Transport};
\node[act, below=of create, xshift=-4cm] (pay) {Thanh toán\\(VNPay/COD)};
\node[act, below=of pay, xshift=4cm] (confirm) {Callback\\xác nhận TT};
\node[act, below=of confirm] (ship) {Giao vận};
\node[dec, below=0.8cm of ship, xshift=-4cm] (d2) {Hoàn trả?};
\node[act, below left=0.8cm and -1cm of d2] (refund) {Tạo yêu cầu\\hoàn trả};
\node[act, below=of refund, xshift=4cm] (review) {Seller duyệt\\hoàn trả};
\node[dec, below=0.8cm of review, xshift=2cm] (d3) {Chấp nhận?};
\node[act, below right=0.8cm and -1cm of d3] (dispute) {Dispute\\(Admin phân xử)};
\node[act, below left=0.8cm and -1cm of d3, xshift=-2cm] (autorefund) {Auto-refund\\qua cổng TT};
\node[finish] (e) at (5.8,-14) {};

\draw[arr] (s) -- (cart);
\draw[arr] (cart) -- (checkout);
\draw[arr] (checkout) -- (reserve);
\draw[arr] (reserve) -- (inbox);
\draw[arr] (inbox) -- (d1);
\draw[arr] (d1) -- node[font=\scriptsize, left] {Không} (reject);
\draw[arr] (d1) -- node[font=\scriptsize, right] {Có} (create);
\draw[arr] (create) -- (pay);
\draw[arr] (pay) -- (confirm);
\draw[arr] (confirm) -- (ship);
\draw[arr] (ship) -- (d2);
\draw[arr] (d2) -- node[font=\scriptsize, left] {Có} (refund);
\draw[arr] (d2.east) -- ++(1,0) |- (e) node[font=\scriptsize, near start, above] {Không};
\draw[arr] (refund) -- (review);
\draw[arr] (review) -- (d3);
\draw[arr] (d3) -- node[font=\scriptsize, left] {Có} (autorefund);
\draw[arr] (d3) -- node[font=\scriptsize, right] {Không} (dispute);
\draw[arr] (autorefund) -- (e);
\draw[arr] (dispute) -- (e);
\draw[arr] (reject) |- (e);
\end{tikzpicture}
}%
\caption{Quy trình đặt hàng, thanh toán, và hoàn trả trong ShopNexus}
\label{fig:order-flow}
\end{figure}
```

---

### Task 6: fig:overall-usecase — Use Case Diagram (3-analysis/usecases-common.tex)

**Files:**
- Modify: `latex/chapters/main/3-analysis/usecases-common.tex` — replace `fig:overall-usecase`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:overall-usecase` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{tikzpicture}[
  uc/.style={ellipse, draw=headerblue, fill=rowgray, text width=2cm, align=center, font=\scriptsize, minimum height=0.55cm, inner sep=1pt},
  actor/.style={font=\scriptsize\bfseries},
  grp/.style={draw=headerblue!40, dashed, rounded corners=4pt, inner sep=4pt},
  arr/.style={-, headerblue!60},
  inc/.style={-{Stealth[length=3pt]}, dashed, headerblue!60},
  ext/.style={-{Stealth[length=3pt]}, dotted, headerblue!60},
  lbl/.style={font=\tiny\itshape, headerblue!70!black},
]

% System boundary
\draw[headerblue, thick, rounded corners=8pt] (-1.5,1) rectangle (11,-17.5);
\node[font=\footnotesize\bfseries, headerblue, anchor=north west] at (-1.3,0.8) {Module Quản lý Đơn hàng};

% Actors
% Buyer stick figure
\node[actor] (buyerlbl) at (-3.2,-5) {Khách hàng};
\draw (-3.2,-5.5) circle(0.2);
\draw (-3.2,-5.7) -- (-3.2,-6.2);
\draw (-3.5,-5.9) -- (-2.9,-5.9);
\draw (-3.2,-6.2) -- (-3.5,-6.6);
\draw (-3.2,-6.2) -- (-2.9,-6.6);

% Seller stick figure
\node[actor] (sellerlbl) at (12.7,-5) {Người bán};
\draw (12.7,-5.5) circle(0.2);
\draw (12.7,-5.7) -- (12.7,-6.2);
\draw (12.4,-5.9) -- (13,-5.9);
\draw (12.7,-6.2) -- (12.4,-6.6);
\draw (12.7,-6.2) -- (13,-6.6);

% Admin stick figure
\node[actor] (adminlbl) at (12.7,-14.5) {Quản trị viên};
\draw (12.7,-15) circle(0.2);
\draw (12.7,-15.2) -- (12.7,-15.7);
\draw (12.4,-15.4) -- (13,-15.4);
\draw (12.7,-15.7) -- (12.4,-16.1);
\draw (12.7,-15.7) -- (13,-16.1);

% --- Use Case Groups ---
% Giỏ hàng
\node[uc] (uc01) at (1.5,0) {Xem giỏ hàng};
\node[uc] (uc02) at (4,0) {Cập nhật giỏ};
\node[uc] (uc03) at (6.5,0) {Xóa giỏ hàng};
\node[grp, fit=(uc01)(uc02)(uc03), label={[lbl]above:Giỏ hàng}] {};

% Đặt hàng
\node[uc] (uc04) at (1.5,-2) {Đặt hàng};
\node[uc] (uc05) at (4,-2) {Mua ngay};
\node[uc] (uc06) at (6.5,-2) {Hủy mục chờ};
\node[grp, fit=(uc04)(uc05)(uc06), label={[lbl]above:Đặt hàng}] {};

% Xử lý đơn (Seller)
\node[uc] (uc07) at (1.5,-4) {Xem mục chờ};
\node[uc] (uc08) at (4,-4) {Báo giá VC};
\node[uc] (uc09) at (6.5,-4) {Xác nhận đơn};
\node[uc] (uc10) at (9,-4) {Từ chối đơn};
\node[grp, fit=(uc07)(uc08)(uc09)(uc10), label={[lbl]above:Xử lý đơn}] {};

% Thanh toán
\node[uc] (uc12) at (2,-6) {Thanh toán};
\node[uc] (uc13) at (5,-6) {Callback TT};
\node[grp, fit=(uc12)(uc13), label={[lbl]above:Thanh toán}] {};

% Theo dõi
\node[uc] (uc14) at (1.5,-8) {Xem đơn\\(Buyer)};
\node[uc] (uc15) at (4,-8) {Xem đơn\\(Seller)};
\node[uc] (uc16) at (7,-8) {Xem DS đơn};
\node[grp, fit=(uc14)(uc15)(uc16), label={[lbl]above:Theo dõi}] {};

% Hoàn trả
\node[uc] (uc17) at (0.5,-10.5) {Xem DS\\hoàn trả};
\node[uc] (uc18) at (3,-10.5) {Tạo hoàn trả};
\node[uc] (uc19) at (5.5,-10.5) {Sửa hoàn trả};
\node[uc] (uc20) at (8,-10.5) {Hủy hoàn trả};
\node[uc] (uc21) at (10,-10.5) {Duyệt hoàn trả};
\node[grp, fit=(uc17)(uc18)(uc19)(uc20)(uc21), label={[lbl]above:Hoàn trả}] {};

% Tranh chấp
\node[uc] (uc22) at (3,-13) {Mở tranh chấp};
\node[uc] (uc23) at (7,-13) {Giải quyết TC};
\node[grp, fit=(uc22)(uc23), label={[lbl]above:Tranh chấp}] {};

% Dashboard
\node[uc] (uc24) at (1,-15) {Thống kê};
\node[uc] (uc25) at (3.5,-15) {Xu hướng};
\node[uc] (uc26) at (6,-15) {Hành động chờ};
\node[uc] (uc27) at (8.5,-15) {SP bán chạy};
\node[grp, fit=(uc24)(uc25)(uc26)(uc27), label={[lbl]above:Dashboard}] {};

% Đánh giá
\node[uc] (uc28) at (1,-17) {Kiểm tra\\quyền ĐG};
\node[uc] (uc29) at (4,-17) {Xem đơn\\ĐG được};
\node[uc] (uc30) at (7,-17) {Xác thực\\đơn cho ĐG};
\node[grp, fit=(uc28)(uc29)(uc30), label={[lbl]above:Đánh giá}] {};

% --- Actor connections ---
% Buyer
\foreach \n in {uc01,uc02,uc03,uc04,uc05,uc06,uc12,uc14,uc16,uc17,uc18,uc19,uc20,uc28,uc29,uc30}
  \draw[arr] (-3.2,-6) -- (\n);
\draw[arr] (-3.2,-6) -- (uc22);

% Seller
\foreach \n in {uc07,uc08,uc09,uc10,uc15,uc16,uc21,uc24,uc25,uc26,uc27}
  \draw[arr] (12.7,-6) -- (\n);
\draw[arr] (12.7,-6) -- (uc22);

% Admin
\draw[arr] (12.7,-15.2) -- (uc23);

% --- Relations ---
% extend: UC05 extends UC04
\draw[ext] (uc05) -- (uc04) node[lbl, midway, above] {\footnotesize$\ll$extend$\gg$};
% include: UC04 includes inventory reservation
\draw[inc] (uc09) -- (uc08) node[lbl, midway, above] {\footnotesize$\ll$include$\gg$};
% extend: UC22 extends UC21
\draw[ext] (uc22) -- (uc21) node[lbl, midway, above] {\footnotesize$\ll$extend$\gg$};

\end{tikzpicture}
}%
\caption{Lược đồ use case tổng quan --- Module quản lý đơn hàng}
\label{fig:overall-usecase}
\end{figure}
```

---

### Task 7: fig:order-sequence (analysis) — Order Sequence Diagram (3-analysis/order.tex)

**Files:**
- Modify: `latex/chapters/main/3-analysis/order.tex` — replace `fig:order-sequence`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:order-sequence` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{sequencediagram}
\newinst{buyer}{Khách hàng}
\newinst[2.5]{shop}{ShopNexus}
\newinst[2]{catalog}{Catalog}
\newinst[1.5]{inv}{Inventory}
\newinst[2]{seller}{Người bán}
\newinst[2]{pgw}{Cổng TT}

\begin{call}{buyer}{checkout(items, buyNow)}{shop}{pendingItems}
  \begin{call}{shop}{getSKU(skuID)}{catalog}{sku}
  \end{call}
  \begin{call}{shop}{reserve(skuID, qty)}{inv}{ok}
  \end{call}
\end{call}

\begin{messcall}{shop}{notify(pendingItems)}{seller}
\end{messcall}

\begin{call}{seller}{confirm(itemIDs, transportOpt)}{shop}{order}
\end{call}

\begin{call}{buyer}{pay(orderIDs, paymentOpt)}{shop}{redirectUrl}
  \begin{call}{shop}{createPayment(amount, opt)}{pgw}{paymentIntent}
  \end{call}
\end{call}

\begin{call}{pgw}{confirmPayment(refID, status)}{shop}{ok}
\end{call}

\begin{messcall}{shop}{notify(orderPaid)}{seller}
\end{messcall}

\end{sequencediagram}
}%
\caption{Lược đồ tuần tự --- Luồng xử lý đơn hàng}
\label{fig:order-sequence}
\end{figure}
```

---

### Task 8: fig:refund-sequence (analysis) — Refund Sequence Diagram (3-analysis/refund.tex)

**Files:**
- Modify: `latex/chapters/main/3-analysis/refund.tex` — replace `fig:refund-sequence`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:refund-sequence` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{sequencediagram}
\newinst{buyer}{Khách hàng}
\newinst[3]{shop}{ShopNexus}
\newinst[3]{seller}{Người bán}
\newinst[3]{admin}{Quản trị viên}

\begin{call}{buyer}{createRefund(orderID, method, reason)}{shop}{refund}
\end{call}

\begin{messcall}{shop}{notify(refundRequested)}{seller}
\end{messcall}

\begin{call}{seller}{confirmRefund(refundID)}{shop}{refund}
\end{call}

\begin{messcall}{shop}{autoRefund(paymentID)}{shop}
\end{messcall}

\begin{messcall}{shop}{notify(refundApproved)}{buyer}
\end{messcall}

\postlevel
\begin{sdblock}{alt}{Seller từ chối}

\begin{call}{buyer}{createDispute(refundID, reason)}{shop}{dispute}
\end{call}

\begin{messcall}{shop}{notify(disputeCreated)}{admin}
\end{messcall}

\begin{call}{admin}{resolveDispute(disputeID, status)}{shop}{result}
\end{call}

\end{sdblock}

\end{sequencediagram}
}%
\caption{Lược đồ tuần tự --- Luồng hoàn trả và tranh chấp}
\label{fig:refund-sequence}
\end{figure}
```

---

### Task 9: fig:architecture — System Architecture Diagram (4-design/architecture.tex)

**Files:**
- Modify: `latex/chapters/main/4-design/architecture.tex` — replace `fig:architecture`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:architecture` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{tikzpicture}[
  mod/.style={rectangle, rounded corners=3pt, draw=headerblue, fill=rowgray, minimum width=1.6cm, minimum height=0.7cm, align=center, font=\scriptsize},
  layer/.style={draw=headerblue!30, fill=headerblue!5, rounded corners=5pt, inner sep=8pt},
  infra/.style={rectangle, rounded corners=3pt, draw=headerblue!60, fill=headerblue!15, minimum width=1.8cm, minimum height=0.6cm, align=center, font=\scriptsize},
  ext/.style={rectangle, rounded corners=3pt, draw=orange!60!black, fill=orange!10, minimum width=1.6cm, minimum height=0.6cm, align=center, font=\scriptsize},
  lbl/.style={font=\scriptsize\bfseries, headerblue},
  arr/.style={-{Stealth[length=4pt]}, thick, headerblue!60},
]

% Transport layer
\node[layer, minimum width=15cm, minimum height=1cm, label={[lbl]left:Transport}] (transport) at (7,6) {};
\node[mod] at (4,6) {Echo v4\\(HTTP)};
\node[mod] at (7,6) {ConnectRPC};
\node[mod] at (10,6) {SSE\\(Realtime)};

% Restate Ingress
\node[rectangle, rounded corners=3pt, draw=headerblue, fill=headerblue!25, minimum width=15cm, minimum height=0.6cm, font=\scriptsize\bfseries, text=headerblue!80!black] (restate) at (7,4.6) {Restate Ingress (Durable Execution --- exactly-once, auto-retry, journal replay)};

% Biz layer
\node[layer, minimum width=15cm, minimum height=1.8cm, label={[lbl]left:Biz}] (biz) at (7,3) {};
\node[mod] (account) at (1.5,3.3)  {account};
\node[mod] (catalog) at (3.5,3.3)  {catalog};
\node[mod, fill=headerblue!20, draw=headerblue, line width=1pt] (order) at (5.5,3.3)  {\textbf{order}};
\node[mod] (inventory) at (7.5,3.3){inventory};
\node[mod] (promotion) at (9.5,3.3){promotion};
\node[mod] (analytic) at (11.5,3.3){analytic};
\node[mod] (chat) at (3.5,2.3)    {chat};
\node[mod] (common) at (5.5,2.3)  {common};

% Data layer
\node[layer, minimum width=15cm, minimum height=1cm, label={[lbl]left:Data}] (data) at (7,0.8) {};
\node[infra] (pg) at (2,0.8)    {PostgreSQL 18};
\node[infra] (redis) at (5,0.8) {Redis 8};
\node[infra] (milvus) at (8,0.8){Milvus 2.6};
\node[infra] (minio) at (11,0.8){MinIO};
\node[infra] (nats) at (13.5,0.8) {NATS};

% External services
\node[ext] (vnpay) at (2,-0.8)   {VNPay};
\node[ext] (ghtk) at (5,-0.8)    {GHTK};
\node[ext] (nominatim) at (8,-0.8){Nominatim};
\node[ext] (openai) at (11,-0.8) {OpenAI};
\node[lbl] at (6.5,-1.4) {Nhà cung cấp dịch vụ bên ngoài};

% Arrows
\draw[arr] (transport) -- (restate);
\draw[arr] (restate) -- (biz);
\draw[arr] (biz) -- (data);
\draw[arr, dashed] (order) -- (vnpay);
\draw[arr, dashed] (order) -- (ghtk);
\draw[arr, dashed] (catalog) -- (openai);

\end{tikzpicture}
}%
\caption{Kiến trúc tổng thể hệ thống ShopNexus}
\label{fig:architecture}
\end{figure}
```

---

### Task 10: fig:order-sequence (design) — Design-Level Order Sequence (4-design/order.tex)

**Files:**
- Modify: `latex/chapters/main/4-design/order.tex` — replace `fig:order-sequence`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for the design-level order sequence with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{sequencediagram}
\newinst{form}{BuyerForm}
\newinst[1.8]{handler}{OrderHandler}
\newinst[1.5]{cat}{CatalogProxy}
\newinst[1.5]{inv}{InventoryProxy}
\newinst[1.5]{pay}{PaymentProvider}
\newinst[1.5]{db}{DB (SQLC)}

\begin{call}{form}{BuyerCheckout(items, buyNow)}{handler}{[]OrderItem}
  \begin{call}{handler}{GetProductSKU(skuID)}{cat}{SKU}
  \end{call}
  \begin{call}{handler}{ReserveInventory(skuID, qty)}{inv}{serialIDs}
  \end{call}
  \begin{call}{handler}{restate.Run: insertItem()}{db}{item}
  \end{call}
  \begin{call}{handler}{restate.Run: deleteCartItem()}{db}{ok}
  \end{call}
\end{call}

\postlevel

\begin{call}{form}{ConfirmSellerPending(itemIDs, opt)}{handler}{Order}
  \begin{call}{handler}{restate.Run: insertOrder()}{db}{order}
  \end{call}
  \begin{call}{handler}{QuoteTransport(items, opt)}{pay}{cost}
  \end{call}
\end{call}

\postlevel

\begin{call}{form}{PayBuyerOrders(orderIDs, opt)}{handler}{redirectUrl}
  \begin{call}{handler}{CreatePayment(amount)}{pay}{intent}
  \end{call}
  \begin{call}{handler}{restate.Run: insertPayment()}{db}{payment}
  \end{call}
\end{call}

\begin{call}{pay}{ConfirmPayment(refID, status)}{handler}{ok}
  \begin{call}{handler}{restate.Run: updateStatus()}{db}{ok}
  \end{call}
\end{call}

\end{sequencediagram}
}%
\caption{Luồng xử lý chính của quy trình mua hàng}
\label{fig:order-sequence}
\end{figure}
```

---

### Task 11: fig:refund-sequence (design) — Design-Level Refund Sequence (4-design/refund.tex)

**Files:**
- Modify: `latex/chapters/main/4-design/refund.tex` — replace `fig:refund-sequence`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for the design-level refund sequence with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{sequencediagram}
\newinst{form}{RefundForm}
\newinst[2]{handler}{OrderHandler}
\newinst[2]{pay}{PaymentProvider}
\newinst[2]{tp}{TransportProvider}
\newinst[2]{db}{DB (SQLC)}

\begin{call}{form}{CreateBuyerRefund(orderID, method, reason)}{handler}{Refund}
  \begin{call}{handler}{restate.Run: getOrder()}{db}{order}
  \end{call}
  \begin{call}{handler}{restate.Run: insertRefund()}{db}{refund}
  \end{call}
\end{call}

\postlevel

\begin{call}{form}{ConfirmSellerRefund(refundID)}{handler}{Refund}
  \begin{call}{handler}{restate.Run: getRefund()}{db}{refund}
  \end{call}
  \begin{call}{handler}{CreateTransport(address, opt)}{tp}{transport}
  \end{call}
  \begin{call}{handler}{Refund(paymentID, amount)}{pay}{ok}
  \end{call}
  \begin{call}{handler}{restate.Run: updateRefund(Success)}{db}{ok}
  \end{call}
  \begin{call}{handler}{restate.Run: restoreInventory()}{db}{ok}
  \end{call}
\end{call}

\end{sequencediagram}
}%
\caption{Luồng xử lý quy trình hoàn trả}
\label{fig:refund-sequence}
\end{figure}
```

---

### Task 12: fig:order-erd — ERD Diagram (4-design/database.tex)

**Files:**
- Modify: `latex/chapters/main/4-design/database.tex` — replace `fig:order-erd`

- [ ] **Step 1: Replace placeholder with TikZ code**

Replace the entire `\begin{figure}...\end{figure}` block for `fig:order-erd` with:

```latex
\begin{figure}[htbp]
\centering
\resizebox{\textwidth}{!}{%
\begin{tikzpicture}[
  entity/.style={rectangle, draw=headerblue, fill=white, minimum width=3.5cm, font=\scriptsize, inner sep=0pt},
  header/.style={rectangle, fill=headerblue, text=white, font=\scriptsize\bfseries, minimum width=3.5cm, minimum height=0.45cm},
  attr/.style={rectangle, fill=rowgray, minimum width=3.5cm, minimum height=0.35cm, font=\scriptsize, anchor=north},
  pk/.style={font=\scriptsize\bfseries},
  fk/.style={font=\scriptsize\itshape},
  enum/.style={rectangle, rounded corners=3pt, draw=headerblue!60, fill=headerblue!8, minimum width=2.2cm, align=left, font=\scriptsize, inner sep=4pt},
  rel/.style={-{Stealth[length=4pt]}, thick, headerblue!70},
  crow/.style={-{Rays[n=3, length=5pt]}, thick, headerblue!70},
]

% === cart_item ===
\node[header] (cart_h) at (0,0) {cart\_item};
\node[attr, anchor=north] (cart1) at (cart_h.south) {\textbf{id} BIGSERIAL PK};
\node[attr, anchor=north] (cart2) at (cart1.south) {account\_id UUID};
\node[attr, anchor=north] (cart3) at (cart2.south) {sku\_id UUID};
\node[attr, anchor=north] (cart4) at (cart3.south) {quantity BIGINT};
\node[entity, fit=(cart_h)(cart4)] {};

% === payment ===
\node[header] (pay_h) at (5,0) {payment};
\node[attr, anchor=north] (pay1) at (pay_h.south) {\textbf{id} BIGSERIAL PK};
\node[attr, anchor=north] (pay2) at (pay1.south) {account\_id UUID};
\node[attr, anchor=north] (pay3) at (pay2.south) {option TEXT};
\node[attr, anchor=north] (pay4) at (pay3.south) {\textit{status} order.status};
\node[attr, anchor=north] (pay5) at (pay4.south) {amount BIGINT};
\node[attr, anchor=north] (pay6) at (pay5.south) {data JSONB};
\node[attr, anchor=north] (pay7) at (pay6.south) {date\_expired TIMESTAMPTZ};
\node[entity, fit=(pay_h)(pay7)] {};

% === transport ===
\node[header] (tp_h) at (10,0) {transport};
\node[attr, anchor=north] (tp1) at (tp_h.south) {\textbf{id} UUID PK};
\node[attr, anchor=north] (tp2) at (tp1.south) {option TEXT};
\node[attr, anchor=north] (tp3) at (tp2.south) {\textit{status} transport\_status};
\node[attr, anchor=north] (tp4) at (tp3.south) {cost BIGINT};
\node[attr, anchor=north] (tp5) at (tp4.south) {data JSONB};
\node[entity, fit=(tp_h)(tp5)] {};

% === order ===
\node[header] (ord_h) at (5,-5.5) {order};
\node[attr, anchor=north] (ord1) at (ord_h.south) {\textbf{id} UUID PK};
\node[attr, anchor=north] (ord2) at (ord1.south) {buyer\_id UUID};
\node[attr, anchor=north] (ord3) at (ord2.south) {seller\_id UUID};
\node[attr, anchor=north] (ord4) at (ord3.south) {\textit{payment\_id} BIGINT FK $\to$ payment};
\node[attr, anchor=north] (ord5) at (ord4.south) {\textit{transport\_id} UUID FK $\to$ transport};
\node[attr, anchor=north] (ord6) at (ord5.south) {product\_cost / discount / transport\_cost};
\node[attr, anchor=north] (ord7) at (ord6.south) {total BIGINT};
\node[attr, anchor=north] (ord8) at (ord7.south) {data JSONB};
\node[entity, fit=(ord_h)(ord8)] {};

% === item ===
\node[header] (item_h) at (0,-5.5) {item};
\node[attr, anchor=north] (item1) at (item_h.south) {\textbf{id} BIGSERIAL PK};
\node[attr, anchor=north] (item2) at (item1.south) {\textit{order\_id} UUID FK $\to$ order};
\node[attr, anchor=north] (item3) at (item2.south) {account\_id, seller\_id UUID};
\node[attr, anchor=north] (item4) at (item3.south) {sku\_id UUID, sku\_name TEXT};
\node[attr, anchor=north] (item5) at (item4.south) {quantity, unit\_price, paid\_amount};
\node[attr, anchor=north] (item6) at (item5.south) {\textit{status} item\_status};
\node[attr, anchor=north] (item7) at (item6.south) {serial\_ids JSONB};
\node[entity, fit=(item_h)(item7)] {};

% === refund ===
\node[header] (ref_h) at (10,-5.5) {refund};
\node[attr, anchor=north] (ref1) at (ref_h.south) {\textbf{id} UUID PK};
\node[attr, anchor=north] (ref2) at (ref1.south) {account\_id UUID};
\node[attr, anchor=north] (ref3) at (ref2.south) {\textit{order\_id} UUID FK $\to$ order};
\node[attr, anchor=north] (ref4) at (ref3.south) {confirmed\_by\_id UUID};
\node[attr, anchor=north] (ref5) at (ref4.south) {\textit{transport\_id} UUID FK $\to$ transport};
\node[attr, anchor=north] (ref6) at (ref5.south) {\textit{method} refund\_method};
\node[attr, anchor=north] (ref7) at (ref6.south) {\textit{status} order.status};
\node[attr, anchor=north] (ref8) at (ref7.south) {reason TEXT};
\node[entity, fit=(ref_h)(ref8)] {};

% === refund_dispute ===
\node[header] (disp_h) at (10,-11) {refund\_dispute};
\node[attr, anchor=north] (disp1) at (disp_h.south) {\textbf{id} UUID PK};
\node[attr, anchor=north] (disp2) at (disp1.south) {\textit{refund\_id} UUID FK $\to$ refund};
\node[attr, anchor=north] (disp3) at (disp2.south) {issued\_by\_id UUID};
\node[attr, anchor=north] (disp4) at (disp3.south) {reason TEXT};
\node[attr, anchor=north] (disp5) at (disp4.south) {\textit{status} order.status};
\node[entity, fit=(disp_h)(disp5)] {};

% === Enums ===
\node[enum] (e1) at (15,0) {\textbf{item\_status}\\Pending\\Confirmed\\Cancelled};
\node[enum] (e2) at (15,-2.5) {\textbf{status}\\Pending\\Processing\\Success\\Cancelled\\Failed};
\node[enum] (e3) at (15,-5.5) {\textbf{transport\_status}\\Pending\\LabelCreated\\InTransit\\OutForDelivery\\Delivered\\Failed\\Cancelled};
\node[enum] (e4) at (15,-9) {\textbf{refund\_method}\\PickUp\\DropOff};

% === Relationships ===
% order -> payment (SET NULL)
\draw[rel] (ord4.east) -- ++(0.3,0) |- (pay7.east) node[near start, right, font=\tiny] {0..1};
% order -> transport (SET NULL)
\draw[rel] (ord5.east) -- ++(1,0) |- (tp5.east) node[near start, right, font=\tiny] {0..1};
% item -> order (CASCADE)
\draw[rel] (item2.east) -- (ord1.west) node[midway, above, font=\tiny] {N:1 CASCADE};
% refund -> order (NO ACTION)
\draw[rel] (ref3.west) -- (ord1.east) node[midway, above, font=\tiny] {N:1 NO ACTION};
% refund -> transport
\draw[rel] (ref5.north) -- ++(0,0.5) -| (tp5.south) node[near end, right, font=\tiny] {0..1};
% dispute -> refund
\draw[rel] (disp2.north) -- ++(0,0.3) -| (ref8.south) node[near end, right, font=\tiny] {N:1};

% Partial index annotation
\node[rectangle, draw=red!50, dashed, rounded corners=2pt, fill=red!5, font=\tiny\itshape, text width=3cm, align=center] at (0,-10.5) {Partial index:\\WHERE order\_id IS NULL\\(pending item inbox)};
\draw[-{Stealth[length=3pt]}, dashed, red!50] (0,-10) -- (item7.south);

\end{tikzpicture}
}%
\caption{Lược đồ ERD của module Order}
\label{fig:order-erd}
\end{figure}
```

---

## Summary

| Task | Diagram | Type | File |
|------|---------|------|------|
| 1 | — | Package setup | thesis.cls |
| 2 | fig:tech-stack | Layer cake | 2-theory.tex |
| 3 | fig:workflow-truyen-thong | Activity | 3-analysis/current-state.tex |
| 4 | fig:workflow-shopnexus | Collaboration | 3-analysis/current-state.tex |
| 5 | fig:order-flow | Activity (swimlane) | 2-theory.tex |
| 6 | fig:overall-usecase | Use case | 3-analysis/usecases-common.tex |
| 7 | fig:order-sequence | Sequence (analysis) | 3-analysis/order.tex |
| 8 | fig:refund-sequence | Sequence (analysis) | 3-analysis/refund.tex |
| 9 | fig:architecture | Architecture | 4-design/architecture.tex |
| 10 | fig:order-sequence | Sequence (design) | 4-design/order.tex |
| 11 | fig:refund-sequence | Sequence (design) | 4-design/refund.tex |
| 12 | fig:order-erd | ERD | 4-design/database.tex |
