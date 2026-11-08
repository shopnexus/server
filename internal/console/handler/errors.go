package handler

import "github.com/shopnexus/server2/internal/platform/apperr"

var ErrNotFromConsole = apperr.New(apperr.KindBadRequest, "invalid_request", "console calls need X-Requested-With: shopnexus-console")
