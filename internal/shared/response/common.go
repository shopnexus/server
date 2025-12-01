package response

import commonmodel "shopnexus-remastered/internal/shared/model"

type CommonResponse struct {
	Data  any                `json:"data,omitempty"`
	Error *commonmodel.Error `json:"error"`
}
