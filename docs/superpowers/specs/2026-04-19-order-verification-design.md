# Order Feature Verification — Code Review & Correctness Audit

**Date**: 2026-04-19
**Scope**: Full order lifecycle (BE + FE) including currency migration
**Method**: Concern-Based Review Matrix (Approach B)

## Objective

Systematically review the entire order feature across backend (Go/Restate) and frontend (Next.js/React) to identify bugs, inconsistencies, and edge cases. Covers the complete lifecycle from cart to dispute resolution, including the recent currency migration (0002_currency).

## Concern Categories (Matrix Columns)

| ID | Concern | Description |
|----|---------|-------------|
| C1 | **Data Consistency** | Fields match across DB schema → Go model → biz layer → HTTP response → FE types |
| C2 | **Error Handling** | Missing error checks, errors not propagated, no rollback on failure |
| C3 | **Race Conditions** | Double submit, concurrent confirm, TOCTOU issues |
| C4 | **FE ↔ BE Mismatch** | Request/response format differences, missing fields, wrong types |
| C5 | **Currency Correctness** | Post-migration units correct, currency/exchange_rate fields populated, FE displays correct units |
| C6 | **State Machine** | Invalid transitions allowed, missing guards, orphan states |
| C7 | **Edge Cases** | Boundary conditions, empty inputs, overflow, zero quantities |
| C8 | **Authorization** | Buyer/seller/admin access scoped correctly, no cross-tenant leaks |

## Flows to Review (Matrix Rows)

| ID | Flow | BE Files | FE Files |
|----|------|----------|----------|
| F1 | **Cart CRUD** | `biz/cart.go`, `transport/echo/cart.go` | `core/order/cart.ts`, `cart/page.tsx` |
| F2 | **Checkout** | `biz/checkout.go`, `transport/echo/order.go` | `core/order/order.buyer.ts`, `checkout/page.tsx` |
| F3 | **Payment Confirm** | `biz/pay.go`, `biz/order.go` (ConfirmPayment) | FE redirect handling |
| F4 | **Seller Pending** | `biz/incoming.go`, `transport/echo/incoming.go` | `core/order/order.seller.ts`, `seller/orders/page.tsx` |
| F5 | **Order Queries** | `biz/order.go`, `transport/echo/order.go` | `core/order/order.buyer.ts`, `order.seller.ts`, order pages |
| F6 | **Transport Updates** | `biz/transport_update.go` | Order detail progress tracker |
| F7 | **Refund** | `biz/refund.go`, `transport/echo/refund.go` | `core/order/refund.buyer.ts`, `refund.seller.ts`, refund dialog |
| F8 | **Dispute** | `biz/dispute.go`, `transport/echo/dispute.go` | FE (if exists) |
| F9 | **Auto-Cancel Timeout** | `biz/timeout.go` | N/A (server-side only) |
| F10 | **Dashboard** | `biz/dashboard.go` | Seller dashboard (if exists) |

## Severity Levels

| Level | Meaning | Example |
|-------|---------|---------|
| `CRITICAL` | Data loss, money loss, security breach | Double charge, refund > order total credited |
| `HIGH` | Logic error, race condition likely to occur | Seller confirms already-cancelled item |
| `MEDIUM` | Inconsistency, missing validation, poor error handling | FE shows wrong field, missing null check |
| `LOW` | Code smell, unlikely edge case | Unused error variant, minor naming mismatch |

## Output Format

### Part 1: Summary Matrix

Table with flows as rows, concerns as columns. Cells show: ✅ OK, ⚠️ Medium/Low, 🔴 Critical/High.

### Part 2: Detailed Findings

Per finding:
- **ID**: `F{flow}-C{concern}-{n}` (e.g., `F2-C3-1`)
- **Severity**: CRITICAL / HIGH / MEDIUM / LOW
- **Flow**: Which flow
- **Concern**: Which concern category
- **Description**: What the issue is
- **Location**: `file_path:line_number` (BE and/or FE)
- **Suggested Fix**: How to resolve

## Execution Plan

1. Dispatch parallel review agents per flow (F1-F10), each checking all 8 concerns
2. Compile findings into summary matrix
3. List detailed findings sorted by severity
4. Present to user for review and prioritization
