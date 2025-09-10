package catalogmodel

import "shopnexus-remastered/internal/db"

type Comment struct {
	db.CatalogComment
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Order    int32  `json:"order" db:"order"`
	Url      string `json:"url" db:"url"`
	MimeType string `json:"mime_type"`
}
