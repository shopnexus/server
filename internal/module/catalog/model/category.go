package catalogmodel

import (
	commonmodel "shopnexus-server/internal/module/common/model"

	"github.com/google/uuid"
	"github.com/guregu/null/v6"
)

type Category struct {
	ID          uuid.UUID              `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	ParentID    null.Int               `json:"parent_id"`
	Resources   []commonmodel.Resource `json:"resources"`
}
