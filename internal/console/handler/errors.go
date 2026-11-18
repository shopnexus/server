package handler

import "github.com/shopnexus/server/internal/platform/apperr"

var ErrNotFromConsole = apperr.New(apperr.KindBadRequest, "invalid_request", "console calls need X-Requested-With: shopnexus-console")
