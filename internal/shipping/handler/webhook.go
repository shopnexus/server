package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/shopnexus/server2/internal/platform/apperr"
	"github.com/shopnexus/server2/internal/platform/httpx"
	"github.com/shopnexus/server2/internal/provider/carrier"
)

// maxReport caps a report's body; GHTK's and GHN's are a few hundred bytes.
const maxReport = 64 << 10

var errNoRoute = apperr.New(apperr.KindNotFound, "not_found", "no such route")

// Webhook serves POST /v1/carriers/{carrier}/webhook/{secret}. The secret
// in the path is the only check: GHTK signs nothing. Once it matches, the
// answer is 200 whatever the report says, a code no order has included,
// since GHTK sends again on anything else and the poll covers a report
// lost here.
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request) {
	c := carrier.Carrier(r.PathValue("carrier"))
	want := h.secrets[c]
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(r.PathValue("secret"))) != 1 {
		httpx.WriteError(w, r, h.log, errNoRoute)
		return
	}
	code, status, moved, err := readReport(c, r)
	if err != nil {
		h.log.WarnContext(r.Context(), "carrier report not read", "carrier", c, "err", err)
		w.WriteHeader(http.StatusOK)
		return
	}
	if moved {
		if err = h.svc.Reported(r.Context(), c, code, status); err != nil {
			httpx.WriteError(w, r, h.log, err)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
}

// readReport reads a report's tracking code and status. moved is false for
// a status that moves no shipment. GHTK posts a form or JSON with label_id
// and status_id; GHN posts JSON with OrderCode and Status.
func readReport(c carrier.Carrier, r *http.Request) (code string, status carrier.Status, moved bool, err error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxReport))
	if err != nil {
		return "", "", false, fmt.Errorf("read the report: %w", err)
	}
	fields := map[string]string{}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media == "application/json" {
		var raw map[string]any
		if err = json.Unmarshal(body, &raw); err != nil {
			return "", "", false, fmt.Errorf("decode the report: %w", err)
		}
		// GHTK sends status_id as 5 or "5".
		for k, v := range raw {
			switch v := v.(type) {
			case string:
				fields[k] = v
			case float64:
				fields[k] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
			}
		}
	} else {
		values, perr := url.ParseQuery(string(body))
		if perr != nil {
			return "", "", false, fmt.Errorf("decode the report: %w", perr)
		}
		for k := range values {
			fields[k] = values.Get(k)
		}
	}
	switch c {
	case carrier.GHTK:
		code = fields["label_id"]
		id, perr := strconv.Atoi(strings.TrimSpace(fields["status_id"]))
		if perr != nil {
			return "", "", false, fmt.Errorf("read status_id: %w", perr)
		}
		status, moved = carrier.GHTKStatus(id)
	case carrier.GHN:
		code = fields["OrderCode"]
		status, moved = carrier.GHNStatus(fields["Status"])
	default:
		return "", "", false, fmt.Errorf("no reports from %s", c)
	}
	if code == "" {
		return "", "", false, errors.New("the report names no code")
	}
	return code, status, moved, nil
}
