package response

import sharedmodel "shopnexus-server/internal/shared/model"

type CommonResponse struct {
	Data  any                `json:"data,omitempty"`
	Error *sharedmodel.Error `json:"error,omitempty"`
}
